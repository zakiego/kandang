package sandbox

import (
	"fmt"
	"sync"
	"unicode/utf8"
)

// Output is a captured stream, capped to keep its head and its tail.
type Output struct {
	Data      []byte
	Truncated bool
	Total     int64 // bytes the program wrote, before the cap
}

// CapWriter keeps the first and the last bytes written to it, so a chatty
// program can't grow kandang's memory, and the end of the output (usually the
// error) survives. It is safe for concurrent use.
type CapWriter struct {
	mu      sync.Mutex
	max     int
	headMax int
	head    []byte
	ring    []byte // the tail, as a ring buffer of max-headMax bytes
	ringPos int
	ringLen int
	total   int64
}

// NewCapWriter returns a writer whose Output is at most max bytes.
func NewCapWriter(max int) *CapWriter {
	if max < 64 {
		max = 64
	}
	head := max / 2
	return &CapWriter{max: max, headMax: head, ring: make([]byte, max-head)}
}

// Write never fails and always reports len(p), so a program never sees an
// error for writing too much.
func (w *CapWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	w.total += int64(n)
	if room := w.headMax - len(w.head); room > 0 {
		k := min(room, len(p))
		w.head = append(w.head, p[:k]...)
		p = p[k:]
	}
	if len(p) >= len(w.ring) {
		copy(w.ring, p[len(p)-len(w.ring):])
		w.ringPos, w.ringLen = 0, len(w.ring)
		return n, nil
	}
	for len(p) > 0 {
		k := copy(w.ring[w.ringPos:], p)
		p = p[k:]
		w.ringPos = (w.ringPos + k) % len(w.ring)
		w.ringLen = min(w.ringLen+k, len(w.ring))
	}
	return n, nil
}

func (w *CapWriter) tail() []byte {
	out := make([]byte, 0, w.ringLen)
	start := (w.ringPos - w.ringLen + len(w.ring)) % len(w.ring)
	if start+w.ringLen <= len(w.ring) {
		return append(out, w.ring[start:start+w.ringLen]...)
	}
	out = append(out, w.ring[start:]...)
	return append(out, w.ring[:w.ringPos]...)
}

// Output returns what was kept. When bytes were cut, a marker saying how many
// sits between the head and the tail, the cut never splits a UTF-8
// character, and the whole result still fits in max bytes.
func (w *CapWriter) Output() Output {
	w.mu.Lock()
	defer w.mu.Unlock()
	tail := w.tail()
	if w.total <= int64(w.max) {
		return Output{Data: append(append([]byte{}, w.head...), tail...), Total: w.total}
	}
	head := trimRuneEnd(w.head)
	tail = trimRuneStart(tail)
	for {
		marker := cutMarker(w.total - int64(len(head)) - int64(len(tail)))
		over := len(head) + len(marker) + len(tail) - w.max
		if over <= 0 {
			data := make([]byte, 0, len(head)+len(marker)+len(tail))
			data = append(append(append(data, head...), marker...), tail...)
			return Output{Data: data, Truncated: true, Total: w.total}
		}
		// Shrink the longer side; the marker may grow a digit, so loop.
		if len(head) == 0 && len(tail) == 0 {
			return Output{Data: []byte(marker)[:w.max], Truncated: true, Total: w.total}
		}
		cut := max(1, (over+1)/2)
		if len(head) > len(tail) {
			head = trimRuneEnd(head[:len(head)-min(cut, len(head))])
		} else {
			tail = trimRuneStart(tail[min(cut, len(tail)):])
		}
	}
}

func cutMarker(n int64) string {
	return fmt.Sprintf("\n\n[... kandang cut %d bytes here ...]\n\n", n)
}

// trimRuneEnd drops an incomplete UTF-8 sequence at the end of b.
func trimRuneEnd(b []byte) []byte {
	for i := 1; i <= utf8.UTFMax && i <= len(b); i++ {
		c := b[len(b)-i]
		if c < 0x80 {
			return b // ASCII: nothing open
		}
		if utf8.RuneStart(c) {
			if r, size := utf8.DecodeRune(b[len(b)-i:]); r == utf8.RuneError && size <= 1 {
				return b[:len(b)-i]
			}
			return b
		}
	}
	return b
}

// trimRuneStart drops continuation bytes at the start of b.
func trimRuneStart(b []byte) []byte {
	for i := 0; i < utf8.UTFMax && i < len(b); i++ {
		if utf8.RuneStart(b[i]) {
			return b[i:]
		}
	}
	return b
}

// Truncate caps b the same way CapWriter does.
func Truncate(b []byte, max int) Output {
	w := NewCapWriter(max)
	w.Write(b)
	return w.Output()
}
