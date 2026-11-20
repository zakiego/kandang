package sandbox

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateShort(t *testing.T) {
	out := Truncate([]byte("hello\n"), MaxStreamBytes)
	if string(out.Data) != "hello\n" || out.Truncated || out.Total != 6 {
		t.Errorf("got %+v", out)
	}
	exact := bytes.Repeat([]byte("x"), MaxStreamBytes)
	if out := Truncate(exact, MaxStreamBytes); out.Truncated || len(out.Data) != MaxStreamBytes {
		t.Errorf("exactly at the cap: truncated=%v len=%d", out.Truncated, len(out.Data))
	}
}

func TestTruncateKeepsHeadAndTail(t *testing.T) {
	var b strings.Builder
	b.WriteString("HEAD-START\n")
	for i := range 20000 {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString("\n")
	}
	b.WriteString("Traceback: the real error\n")
	in := b.String()

	out := Truncate([]byte(in), MaxStreamBytes)
	s := string(out.Data)
	if !out.Truncated {
		t.Fatal("not truncated")
	}
	if len(out.Data) > MaxStreamBytes {
		t.Errorf("len = %d, over the %d cap", len(out.Data), MaxStreamBytes)
	}
	if !strings.HasPrefix(s, "HEAD-START\n") {
		t.Error("lost the head")
	}
	if !strings.HasSuffix(s, "Traceback: the real error\n") {
		t.Error("lost the tail")
	}
	if !strings.Contains(s, "[... kandang cut ") {
		t.Error("no marker")
	}
	if out.Total != int64(len(in)) {
		t.Errorf("Total = %d, want %d", out.Total, len(in))
	}
}

func TestCapWriterManySmallWrites(t *testing.T) {
	w := NewCapWriter(100)
	var all bytes.Buffer
	for i := range 1000 {
		p := []byte{byte('a' + i%26)}
		all.Write(p)
		if n, err := w.Write(p); n != 1 || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	out := w.Output()
	full := all.Bytes()
	if !out.Truncated || len(out.Data) > 100 {
		t.Fatalf("got %d bytes, truncated=%v", len(out.Data), out.Truncated)
	}
	head := out.Data[:bytes.Index(out.Data, []byte("\n\n[... kandang cut"))]
	if len(head) == 0 || !bytes.HasPrefix(full, head) {
		t.Errorf("head %q isn't the start of the input", head)
	}
	// The tail after the marker must be the real last bytes.
	idx := bytes.LastIndex(out.Data, []byte("here ...]\n\n"))
	tail := out.Data[idx+len("here ...]\n\n"):]
	if !bytes.HasSuffix(full, tail) || len(tail) == 0 {
		t.Errorf("tail %q isn't the end of the input", tail)
	}
}

func TestTruncateUTF8(t *testing.T) {
	in := []byte(strings.Repeat("é€😀", 10000))
	out := Truncate(in, 1000)
	if !utf8.Valid(out.Data) {
		t.Error("truncated output isn't valid UTF-8")
	}
	if len(out.Data) > 1000 {
		t.Errorf("len = %d", len(out.Data))
	}
}

func TestCapWriterLargeSingleWrite(t *testing.T) {
	w := NewCapWriter(64)
	_, _ = w.Write([]byte("0123456789"))
	_, _ = w.Write(bytes.Repeat([]byte("m"), 10000))
	_, _ = w.Write([]byte("END"))
	out := w.Output()
	if !bytes.HasPrefix(out.Data, []byte("0123456789")) || !bytes.HasSuffix(out.Data, []byte("END")) {
		t.Errorf("got %q", out.Data)
	}
	if len(out.Data) > 64 {
		t.Errorf("len = %d", len(out.Data))
	}
}
