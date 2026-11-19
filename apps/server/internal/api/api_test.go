package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zakiego/kandang/apps/server/internal/queue"
	"github.com/zakiego/kandang/apps/server/internal/sandbox"
)

const testToken = "test-token-0123456789abcdefghijklmnopqrstuvwxyz"

type fakeRunner struct {
	mu     sync.Mutex
	ready  bool
	jobs   []sandbox.Job
	result sandbox.Result
	err    error
	block  chan struct{} // when set, Run waits on it (or ctx)
	start  chan struct{} // when set, Run signals here as it starts
}

func (f *fakeRunner) Run(ctx context.Context, job sandbox.Job) (sandbox.Result, error) {
	f.mu.Lock()
	f.jobs = append(f.jobs, job)
	block, start := f.block, f.start
	f.mu.Unlock()
	if start != nil {
		start <- struct{}{}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return sandbox.Result{}, ctx.Err()
		}
	}
	return f.result, f.err
}

func (f *fakeRunner) Status() sandbox.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.ready {
		return sandbox.Status{Ready: false, Runsc: "missing: exec: \"runsc\": not found", Rootfs: "ok"}
	}
	return sandbox.Status{Ready: true, Runsc: "release-20261116.0", Rootfs: "ok"}
}

func newTest(t *testing.T, f *fakeRunner, q *queue.Queue) http.Handler {
	t.Helper()
	if q == nil {
		q = queue.New(2, 10)
	}
	return New(Options{
		Runner: f, Queue: q, Token: []byte(testToken), Version: "v0.1.0-test",
		Limits: sandbox.DefaultLimits(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		NewID: func() string { return "AAAAAAAAAAAAAAAAAAAAAA" },
	})
}

func do(h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	case []byte:
		r = bytes.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		r = bytes.NewReader(j)
	}
	req := httptest.NewRequest(method, path, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var e ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("error body isn't JSON: %s", w.Body)
	}
	if e.Message == "" {
		t.Errorf("error %q has no message", e.Error)
	}
	return e.Error
}

func TestHealth(t *testing.T) {
	f := &fakeRunner{ready: true}
	w := do(newTest(t, f, nil), "GET", "/health", "", nil)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var h map[string]any
	json.Unmarshal(w.Body.Bytes(), &h)
	if h["ok"] != true || h["version"] != "v0.1.0-test" || h["runsc"] != "release-20261116.0" || h["rootfs"] != "ok" {
		t.Errorf("health = %v", h)
	}
	if q, _ := h["queue"].(map[string]any); q["running"] != 0.0 || q["waiting"] != 0.0 {
		t.Errorf("queue = %v", h["queue"])
	}

	f.ready = false
	w = do(newTest(t, f, nil), "GET", "/health", "", nil)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"ok":false`) {
		t.Errorf("not ready: %d %s", w.Code, w.Body)
	}
}

func TestAuth(t *testing.T) {
	h := newTest(t, &fakeRunner{ready: true}, nil)
	body := map[string]any{"language": "python", "code": "print(1)"}
	for _, tok := range []string{"", "wrong", testToken + "x", testToken[:10]} {
		w := do(h, "POST", "/v1/runs", tok, body)
		if w.Code != 401 || errCode(t, w) != "unauthorized" {
			t.Errorf("token %q: %d %s", tok, w.Code, w.Body)
		}
	}
	req := httptest.NewRequest("POST", "/v1/runs", strings.NewReader(`{"language":"bash","code":"true"}`))
	req.Header.Set("Authorization", "Basic "+testToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("Basic scheme: %d", w.Code)
	}
	req = httptest.NewRequest("POST", "/v1/runs", strings.NewReader(`{"language":"bash","code":"true"}`))
	req.Header.Set("Authorization", "bearer "+testToken)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("lowercase bearer: %d %s", w.Code, w.Body)
	}

	empty := New(Options{Runner: &fakeRunner{ready: true}, Queue: queue.New(1, 1), Limits: sandbox.DefaultLimits()})
	if w := do(empty, "POST", "/v1/runs", "", body); w.Code != 401 {
		t.Errorf("no token configured must refuse everyone, got %d", w.Code)
	}
}

func TestRunOK(t *testing.T) {
	f := &fakeRunner{ready: true, result: sandbox.Result{
		ExitCode:        0,
		Duration:        1234 * time.Millisecond,
		PeakMemoryBytes: 225 << 20,
		Stdout:          sandbox.Output{Data: []byte("hello\n")},
		Stderr:          sandbox.Truncate(bytes.Repeat([]byte("e"), 40000), sandbox.MaxStreamBytes),
		Files:           []sandbox.File{{Name: "chart.png", Data: []byte("\x89PNG")}},
		Dropped:         []sandbox.Dropped{{Name: "sub", Reason: sandbox.DropDirectory}},
	}}
	h := newTest(t, f, nil)
	w := do(h, "POST", "/v1/runs", testToken, map[string]any{
		"language":   "python",
		"code":       "print('hello')",
		"timeoutSec": 5,
		"files":      []map[string]string{{"name": "data/q1.csv", "content": base64.StdEncoding.EncodeToString([]byte("a,b\n"))}},
	})
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var res map[string]any
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["exitCode"] != 0.0 || res["timedOut"] != false || res["durationMs"] != 1234.0 || res["peakMemoryMb"] != 225.0 {
		t.Errorf("res = %v", res)
	}
	if res["stdout"] != "hello\n" || res["stdoutTruncated"] != false || res["stderrTruncated"] != true {
		t.Errorf("streams = %v %v %v", res["stdout"], res["stdoutTruncated"], res["stderrTruncated"])
	}
	files := res["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	f0 := files[0].(map[string]any)
	if f0["name"] != "chart.png" || f0["size"] != 4.0 || f0["content"] != base64.StdEncoding.EncodeToString([]byte("\x89PNG")) {
		t.Errorf("file = %v", f0)
	}
	if d := res["filesDropped"].([]any); len(d) != 1 || d[0].(map[string]any)["name"] != "sub" {
		t.Errorf("filesDropped = %v", d)
	}

	job := f.jobs[0]
	if job.ID != "AAAAAAAAAAAAAAAAAAAAAA" || job.Language != "python" || string(job.Code) != "print('hello')" || job.Timeout != 5*time.Second {
		t.Errorf("job = %+v", job)
	}
	if len(job.Files) != 1 || job.Files[0].Name != "q1.csv" || string(job.Files[0].Data) != "a,b\n" {
		t.Errorf("job files = %+v", job.Files)
	}
}

func TestRunDefaultsAndEmptyFiles(t *testing.T) {
	f := &fakeRunner{ready: true}
	w := do(newTest(t, f, nil), "POST", "/v1/runs", testToken, `{"language":"bash","code":"echo hi"}`)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if f.jobs[0].Timeout != 60*time.Second {
		t.Errorf("default timeout = %s", f.jobs[0].Timeout)
	}
	if !strings.Contains(w.Body.String(), `"files":[]`) {
		t.Errorf("files should be [] not null: %s", w.Body)
	}
	if strings.Contains(w.Body.String(), "filesDropped") {
		t.Errorf("filesDropped should be left out when empty: %s", w.Body)
	}
}

func TestValidation(t *testing.T) {
	b64 := func(n int) string { return base64.StdEncoding.EncodeToString(make([]byte, n)) }
	manyFiles := []map[string]string{}
	for i := range 21 {
		manyFiles = append(manyFiles, map[string]string{"name": fmt.Sprintf("f%d", i), "content": ""})
	}
	fourBig := []map[string]string{}
	for i := range 6 { // 51 MiB: over the file total, under the body cap
		fourBig = append(fourBig, map[string]string{"name": fmt.Sprintf("f%d", i), "content": b64(17 << 19)})
	}
	cases := []struct {
		name   string
		body   any
		status int
		code   string
	}{
		{"not json", `{"language":`, 400, "invalid_json"},
		{"array", `[]`, 400, "invalid_json"},
		{"unknown field", `{"language":"bash","code":"x","image":"ubuntu"}`, 400, "invalid_json"},
		{"trailing data", `{"language":"bash","code":"x"} {}`, 400, "invalid_json"},
		{"no language", map[string]any{"code": "x"}, 400, "invalid_language"},
		{"bad language", map[string]any{"language": "ruby", "code": "x"}, 400, "invalid_language"},
		{"no code", map[string]any{"language": "bash"}, 400, "invalid_code"},
		{"blank code", map[string]any{"language": "bash", "code": "  \n"}, 400, "invalid_code"},
		{"nul in code", map[string]any{"language": "bash", "code": "a\x00b"}, 400, "invalid_code"},
		{"code too big", map[string]any{"language": "bash", "code": strings.Repeat("x", 100<<10+1)}, 413, "code_too_large"},
		{"timeout zero", map[string]any{"language": "bash", "code": "x", "timeoutSec": 0}, 400, "invalid_timeout"},
		{"timeout negative", map[string]any{"language": "bash", "code": "x", "timeoutSec": -1}, 400, "invalid_timeout"},
		{"timeout too long", map[string]any{"language": "bash", "code": "x", "timeoutSec": 301}, 400, "invalid_timeout"},
		{"timeout string", `{"language":"bash","code":"x","timeoutSec":"5"}`, 400, "invalid_json"},
		{"21 files", map[string]any{"language": "bash", "code": "x", "files": manyFiles}, 400, "too_many_files"},
		{"dotdot", map[string]any{"language": "bash", "code": "x", "files": []map[string]string{{"name": "../etc/passwd", "content": ""}}}, 400, "invalid_file"},
		{"empty name", map[string]any{"language": "bash", "code": "x", "files": []map[string]string{{"name": "", "content": ""}}}, 400, "invalid_file"},
		{"control char", map[string]any{"language": "bash", "code": "x", "files": []map[string]string{{"name": "a\x01b", "content": ""}}}, 400, "invalid_file"},
		{"long name", map[string]any{"language": "bash", "code": "x", "files": []map[string]string{{"name": strings.Repeat("n", 129), "content": ""}}}, 400, "invalid_file"},
		{"dup after clean", map[string]any{"language": "bash", "code": "x", "files": []map[string]string{{"name": "a/x.csv", "content": ""}, {"name": "b/x.csv", "content": ""}}}, 400, "invalid_file"},
		{"bad base64", map[string]any{"language": "bash", "code": "x", "files": []map[string]string{{"name": "x", "content": "!!!"}}}, 400, "invalid_file"},
		{"file too big", map[string]any{"language": "bash", "code": "x", "files": []map[string]string{{"name": "x", "content": b64(15<<20 + 1)}}}, 413, "file_too_large"},
		{"total too big", map[string]any{"language": "bash", "code": "x", "files": fourBig}, 413, "files_too_large"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeRunner{ready: true}
			w := do(newTest(t, f, nil), "POST", "/v1/runs", testToken, c.body)
			if w.Code != c.status {
				t.Fatalf("status %d, want %d: %s", w.Code, c.status, w.Body)
			}
			if got := errCode(t, w); got != c.code {
				t.Errorf("error %q, want %q", got, c.code)
			}
			if len(f.jobs) != 0 {
				t.Error("an invalid request reached the runner")
			}
		})
	}
}

func TestFileAtExactLimits(t *testing.T) {
	f := &fakeRunner{ready: true}
	body := map[string]any{"language": "bash", "code": "x", "files": []map[string]string{
		{"name": "a", "content": base64.StdEncoding.EncodeToString(make([]byte, 15<<20))},
	}}
	if w := do(newTest(t, f, nil), "POST", "/v1/runs", testToken, body); w.Code != 200 {
		t.Fatalf("15 MiB file: %d %s", w.Code, w.Body)
	}
}

func TestBodyLimit(t *testing.T) {
	h := newTest(t, &fakeRunner{ready: true}, nil)
	huge := `{"language":"bash","code":"x","files":[{"name":"a","content":"` + strings.Repeat("A", MaxBodyBytes) + `"}]}`
	w := do(h, "POST", "/v1/runs", testToken, huge)
	if w.Code != 413 || errCode(t, w) != "body_too_large" {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	// Without a Content-Length the reader limit still catches it.
	req := httptest.NewRequest("POST", "/v1/runs", io.MultiReader(strings.NewReader(huge)))
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 413 {
		t.Errorf("chunked: %d %s", rec.Code, rec.Body)
	}
}

func TestNotReady(t *testing.T) {
	f := &fakeRunner{ready: false}
	w := do(newTest(t, f, nil), "POST", "/v1/runs", testToken, `{"language":"bash","code":"x"}`)
	if w.Code != 503 || errCode(t, w) != "not_ready" {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	f2 := &fakeRunner{ready: true, err: fmt.Errorf("wrapped: %w", sandbox.ErrNotReady)}
	if w := do(newTest(t, f2, nil), "POST", "/v1/runs", testToken, `{"language":"bash","code":"x"}`); w.Code != 503 {
		t.Errorf("runner ErrNotReady: %d", w.Code)
	}
}

func TestRunnerFailure(t *testing.T) {
	f := &fakeRunner{ready: true, err: errors.New("mount: permission denied at /var/lib/kandang/state")}
	w := do(newTest(t, f, nil), "POST", "/v1/runs", testToken, `{"language":"bash","code":"x"}`)
	if w.Code != 500 || errCode(t, w) != "internal" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "/var/lib") {
		t.Error("internal error details leaked to the caller")
	}
}

func TestBusy(t *testing.T) {
	f := &fakeRunner{ready: true, block: make(chan struct{}), start: make(chan struct{}, 2)}
	q := queue.New(2, 10)
	h := newTest(t, f, q)
	body := `{"language":"bash","code":"sleep 1"}`

	var wg sync.WaitGroup
	codes := make(chan int, 12)
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- do(h, "POST", "/v1/runs", testToken, body).Code
		}()
		if i < 2 {
			<-f.start
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for q.Stats() != (queue.Stats{Running: 2, Waiting: 10}) {
		if time.Now().After(deadline) {
			t.Fatalf("stats = %+v", q.Stats())
		}
		time.Sleep(time.Millisecond)
	}
	hw := do(h, "GET", "/health", "", nil)
	if !strings.Contains(hw.Body.String(), `"queue":{"running":2,"waiting":10}`) {
		t.Errorf("health = %s", hw.Body)
	}

	w := do(h, "POST", "/v1/runs", testToken, body)
	if w.Code != 429 || errCode(t, w) != "busy" || w.Header().Get("Retry-After") == "" {
		t.Errorf("13th: %d %s", w.Code, w.Body)
	}

	f.mu.Lock()
	f.start = nil
	f.mu.Unlock()
	close(f.block)
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != 200 {
			t.Errorf("queued run got %d", c)
		}
	}
}

func TestNotFound(t *testing.T) {
	h := newTest(t, &fakeRunner{ready: true}, nil)
	for _, p := range []struct{ m, path string }{{"GET", "/"}, {"GET", "/v1/runs"}, {"POST", "/v1/exec"}, {"DELETE", "/health"}} {
		w := do(h, p.m, p.path, testToken, nil)
		if w.Code != 404 && w.Code != 405 {
			t.Errorf("%s %s = %d", p.m, p.path, w.Code)
		}
	}
}

func TestRunResponseShape(t *testing.T) {
	big := bytes.Repeat([]byte("kandang"), 100000)
	res := sandbox.Result{
		ExitCode: 3, TimedOut: true, Duration: 1500 * time.Millisecond, PeakMemoryBytes: 10 << 20,
		Stdout: sandbox.Output{Data: []byte("a<b>&\"q\"\n")},
		Stderr: sandbox.Output{Data: []byte("warn\n"), Truncated: true},
		Files: []sandbox.File{
			{Name: `we"ird <name>.txt`, Data: []byte{0, 1, 2, 255}},
			{Name: "empty", Data: nil},
			{Name: "big.bin", Data: bytes.Clone(big)},
		},
		Dropped: []sandbox.Dropped{{Name: "sub", Reason: sandbox.DropDirectory}},
	}
	w := httptest.NewRecorder()
	if err := writeRun(w, res); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("status %d, headers %v", w.Code, w.Header())
	}
	if !strings.Contains(w.Body.String(), `"stdout":"a<b>&\"q\"\n"`) {
		t.Errorf("stdout should not be HTML-escaped: %.200s", w.Body)
	}
	var got RunResponse
	dec := json.NewDecoder(w.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("the streamed response isn't a RunResponse: %v", err)
	}
	if got.ExitCode != 3 || !got.TimedOut || got.DurationMs != 1500 || got.PeakMemoryMb != 10 ||
		got.Stdout != "a<b>&\"q\"\n" || got.Stderr != "warn\n" || got.StdoutTruncated || !got.StderrTruncated {
		t.Errorf("head = %+v", got)
	}
	want := []struct {
		name string
		data []byte
	}{{`we"ird <name>.txt`, []byte{0, 1, 2, 255}}, {"empty", nil}, {"big.bin", big}}
	if len(got.Files) != len(want) {
		t.Fatalf("files = %d, want %d", len(got.Files), len(want))
	}
	for i, wf := range want {
		data, err := base64.StdEncoding.DecodeString(got.Files[i].Content)
		if err != nil || got.Files[i].Name != wf.name || got.Files[i].Size != len(wf.data) || !bytes.Equal(data, wf.data) {
			t.Errorf("files[%d] = %q size %d (%v)", i, got.Files[i].Name, got.Files[i].Size, err)
		}
	}
	if len(got.FilesDropped) != 1 || got.FilesDropped[0] != (DroppedFile{Name: "sub", Reason: sandbox.DropDirectory}) {
		t.Errorf("filesDropped = %+v", got.FilesDropped)
	}
}

// discard keeps nothing, so the test below measures what writing a response
// allocates, not what a recorder stores.
type discard struct {
	h http.Header
	n int64
}

func (d *discard) Header() http.Header         { return d.h }
func (d *discard) WriteHeader(int)             {}
func (d *discard) Write(p []byte) (int, error) { d.n += int64(len(p)); return len(p), nil }

// The code in the sandbox decides how much comes back, up to 10 files of
// 15 MiB. Sending that must not cost kandang a second and third copy.
func TestRunResponseIsStreamed(t *testing.T) {
	blob := make([]byte, sandbox.MaxOutputFileBytes)
	res := sandbox.Result{}
	for i := range sandbox.MaxOutputFiles {
		res.Files = append(res.Files, sandbox.File{Name: fmt.Sprintf("f%d.bin", i), Data: blob})
	}
	d := &discard{h: http.Header{}}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if err := writeRun(d, res); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if got := (after.TotalAlloc - before.TotalAlloc) >> 20; got > 8 {
		t.Errorf("allocated %d MiB to send 150 MiB of files; the response must be streamed", got)
	}
	if d.n < 200<<20 {
		t.Errorf("wrote %d bytes, want the base64 of 150 MiB", d.n)
	}
	for _, f := range res.Files {
		if f.Data != nil {
			t.Fatalf("%s is still held after it was written", f.Name)
		}
	}
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.deadlines = append(d.deadlines, t)
	return nil
}

// A client that stops reading a response would hold its run slot forever.
// The write gets a deadline, and the deadline is cleared again so it can't
// cut off a later, longer run on the same connection.
func TestRunResponseHasAWriteDeadline(t *testing.T) {
	h := newTest(t, &fakeRunner{ready: true}, nil)
	req := httptest.NewRequest("POST", "/v1/runs", strings.NewReader(`{"language":"bash","code":"true"}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(w.deadlines) != 2 {
		t.Fatalf("write deadlines set: %v, want one for the write and one to clear it", w.deadlines)
	}
	if left := time.Until(w.deadlines[0]); left <= 0 || left > ResponseWriteTimeout {
		t.Errorf("write deadline is %s away, want within %s", left, ResponseWriteTimeout)
	}
	if !w.deadlines[1].IsZero() {
		t.Errorf("the deadline wasn't cleared after the response: %v", w.deadlines[1])
	}
}
