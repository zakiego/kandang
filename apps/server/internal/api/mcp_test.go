package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zakiego/kandang/apps/server/internal/queue"
	"github.com/zakiego/kandang/apps/server/internal/sandbox"
)

// pngBytes is the start of a real PNG, enough for content sniffing.
var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")

type rpcReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

func rpc(id any, method string, params any) map[string]any {
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		m["id"] = id
	}
	if params != nil {
		m["params"] = params
	}
	return m
}

func decodeReply(t *testing.T, w *httptest.ResponseRecorder) rpcReply {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type %q", ct)
	}
	var r rpcReply
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("reply isn't JSON: %s", w.Body)
	}
	if r.JSONRPC != "2.0" {
		t.Errorf("jsonrpc %q", r.JSONRPC)
	}
	return r
}

type callResult struct {
	Content []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Data     string `json:"data"`
		MimeType string `json:"mimeType"`
		Resource struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Blob     string `json:"blob"`
		} `json:"resource"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

func callRun(t *testing.T, h http.Handler, args any) callResult {
	t.Helper()
	r := decodeReply(t, do(h, "POST", "/mcp", testToken, rpc(7, "tools/call", map[string]any{"name": "run", "arguments": args})))
	if r.Error != nil {
		t.Fatalf("rpc error %d: %s", r.Error.Code, r.Error.Message)
	}
	if string(r.ID) != "7" {
		t.Errorf("id %s, want 7", r.ID)
	}
	var res callResult
	if err := json.Unmarshal(r.Result, &res); err != nil {
		t.Fatalf("result isn't a tool result: %s", r.Result)
	}
	if len(res.Content) == 0 || res.Content[0].Type != "text" {
		t.Fatalf("first content block isn't text: %s", r.Result)
	}
	return res
}

func TestMCPTransport(t *testing.T) {
	h := newTest(t, &fakeRunner{ready: true}, nil)
	init := rpc(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"})

	if w := do(h, "GET", "/mcp", testToken, nil); w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "POST" {
		t.Errorf("GET: %d, Allow %q", w.Code, w.Header().Get("Allow"))
	}
	if w := do(h, "DELETE", "/mcp", testToken, nil); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE: %d", w.Code)
	}
	if w := do(h, "POST", "/mcp", "", init); w.Code != http.StatusUnauthorized || errCode(t, w) != "unauthorized" {
		t.Errorf("no token: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/mcp", "wrong-token", init); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", w.Code)
	}

	j, _ := json.Marshal(init)
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(j)))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || errCode(t, w) != "forbidden_origin" {
		t.Errorf("Origin: %d %s", w.Code, w.Body)
	}

	req = httptest.NewRequest("POST", "/mcp", strings.NewReader(string(j)))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("MCP-Protocol-Version", "1999-01-01")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || errCode(t, w) != "unsupported_protocol_version" {
		t.Errorf("bad protocol header: %d %s", w.Code, w.Body)
	}

	if w := do(h, "POST", "/mcp", testToken, rpc(nil, "notifications/initialized", nil)); w.Code != http.StatusAccepted || w.Body.Len() != 0 {
		t.Errorf("notification: %d %q", w.Code, w.Body)
	}
	if w := do(h, "POST", "/mcp", testToken, `{"jsonrpc":"2.0","id":3,"result":{}}`); w.Code != http.StatusAccepted {
		t.Errorf("client response: %d", w.Code)
	}

	for name, tc := range map[string]struct {
		body string
		code int
	}{
		"not json":       {`{"jsonrpc":`, rpcParseError},
		"batch":          {`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, rpcInvalidRequest},
		"old jsonrpc":    {`{"jsonrpc":"1.0","id":1,"method":"ping"}`, rpcInvalidRequest},
		"null id":        {`{"jsonrpc":"2.0","id":null,"method":"ping"}`, rpcInvalidRequest},
		"object id":      {`{"jsonrpc":"2.0","id":{},"method":"ping"}`, rpcInvalidRequest},
		"trailing data":  {`{"jsonrpc":"2.0","id":1,"method":"ping"} {}`, rpcParseError},
		"unknown method": {`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`, rpcMethodNotFound},
		"discover":       {`{"jsonrpc":"2.0","id":1,"method":"server/discover"}`, rpcMethodNotFound},
		"unknown tool":   {`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"exec","arguments":{}}}`, rpcInvalidParams},
		"bad params":     {`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":[]}`, rpcInvalidParams},
	} {
		r := decodeReply(t, do(h, "POST", "/mcp", testToken, tc.body))
		if r.Error == nil || r.Error.Code != tc.code || r.Error.Message == "" {
			t.Errorf("%s: error %+v, want code %d", name, r.Error, tc.code)
		}
	}

	r := decodeReply(t, do(h, "POST", "/mcp", testToken, rpc("p", "ping", nil)))
	if string(r.ID) != `"p"` || string(r.Result) != "{}" {
		t.Errorf("ping: id %s result %s", r.ID, r.Result)
	}
}

func TestMCPInitialize(t *testing.T) {
	h := newTest(t, &fakeRunner{ready: true}, nil)
	for asked, want := range map[string]string{
		"2025-06-18": "2025-06-18",
		"2025-11-25": "2025-11-25",
		"2026-07-28": "2025-11-25",
		"":           "2025-11-25",
	} {
		r := decodeReply(t, do(h, "POST", "/mcp", testToken, rpc(1, "initialize", map[string]any{
			"protocolVersion": asked, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"},
		})))
		var res mcpInitializeResult
		if err := json.Unmarshal(r.Result, &res); err != nil || r.Error != nil {
			t.Fatalf("initialize %q: %s %+v", asked, r.Result, r.Error)
		}
		if res.ProtocolVersion != want {
			t.Errorf("asked %q, got %q, want %q", asked, res.ProtocolVersion, want)
		}
		if res.ServerInfo.Name != "kandang" || res.ServerInfo.Version != "v0.1.0-test" || res.Instructions == "" {
			t.Errorf("initialize result: %+v", res)
		}
		if _, ok := res.Capabilities["tools"]; !ok {
			t.Errorf("no tools capability: %s", r.Result)
		}
	}
}

func TestMCPToolsList(t *testing.T) {
	h := newTest(t, &fakeRunner{ready: true}, nil)
	r := decodeReply(t, do(h, "POST", "/mcp", testToken, rpc(2, "tools/list", nil)))
	var res struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
			Annotations map[string]any `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil {
		t.Fatalf("tools/list: %s", r.Result)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "run" {
		t.Fatalf("tools: %s", r.Result)
	}
	tool := res.Tools[0]
	if tool.InputSchema["type"] != "object" || tool.Description == "" {
		t.Errorf("tool: %+v", tool)
	}
	props, _ := tool.InputSchema["properties"].(map[string]any)
	timeout, _ := props["timeoutSec"].(map[string]any)
	if timeout["maximum"] != float64(300) {
		t.Errorf("timeoutSec schema: %v", timeout)
	}
	if tool.Annotations["readOnlyHint"] != true || tool.Annotations["openWorldHint"] != false {
		t.Errorf("annotations: %v", tool.Annotations)
	}
}

func TestMCPRun(t *testing.T) {
	big := []byte(strings.Repeat("x", 100))
	f := &fakeRunner{ready: true, result: sandbox.Result{
		ExitCode: 0, Duration: 412 * time.Millisecond, PeakMemoryBytes: 21 << 20,
		Stdout: sandbox.Output{Data: []byte("45\n")},
		Files: []sandbox.File{
			{Name: "chart.png", Data: pngBytes},
			{Name: "data.csv", Data: []byte("a,b\n1,2\n")},
			{Name: "fake.png", Data: big},
			{Name: "book.xlsx", Data: []byte("PK\x03\x04\xff\xfe")},
		},
		Dropped: []sandbox.Dropped{{Name: "sub", Reason: sandbox.DropDirectory}},
	}}
	h := newTest(t, f, nil)
	res := callRun(t, h, map[string]any{
		"language": "python", "code": "print(45)", "timeoutSec": 5,
		"files": []map[string]any{{"name": "in.txt", "content": base64.StdEncoding.EncodeToString([]byte("hi"))}},
	})
	if res.IsError {
		t.Errorf("isError on exit 0")
	}
	if len(f.jobs) != 1 {
		t.Fatalf("%d jobs ran", len(f.jobs))
	}
	job := f.jobs[0]
	if job.Language != "python" || string(job.Code) != "print(45)" || job.Timeout != 5*time.Second ||
		len(job.Files) != 1 || string(job.Files[0].Data) != "hi" || job.ID == "" {
		t.Errorf("job: %+v", job)
	}

	var sum struct {
		ExitCode     int           `json:"exitCode"`
		Stdout       string        `json:"stdout"`
		Files        []mcpFileNote `json:"files"`
		FilesDropped []DroppedFile `json:"filesDropped"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &sum); err != nil {
		t.Fatalf("summary isn't JSON: %s", res.Content[0].Text)
	}
	if sum.Stdout != "45\n" || len(sum.Files) != 4 || len(sum.FilesDropped) != 1 {
		t.Errorf("summary: %+v", sum)
	}
	if len(res.Content) != 5 {
		t.Fatalf("%d content blocks, want 5", len(res.Content))
	}

	img := res.Content[1]
	if img.Type != "image" || img.MimeType != "image/png" || img.Data != base64.StdEncoding.EncodeToString(pngBytes) {
		t.Errorf("chart.png: %+v", img)
	}
	csv := res.Content[2]
	if csv.Type != "resource" || csv.Resource.MimeType != "text/csv" || csv.Resource.Text != "a,b\n1,2\n" || csv.Resource.Blob != "" ||
		csv.Resource.URI != "kandang://runs/AAAAAAAAAAAAAAAAAAAAAA/out/data.csv" {
		t.Errorf("data.csv: %+v", csv)
	}
	// A .png that isn't one is never handed over as an image.
	fake := res.Content[3]
	if fake.Type != "resource" || fake.Resource.MimeType != "text/plain" {
		t.Errorf("fake.png: %+v", fake)
	}
	xlsx := res.Content[4]
	if xlsx.Type != "resource" || xlsx.Resource.Blob == "" || xlsx.Resource.Text != "" ||
		xlsx.Resource.MimeType != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Errorf("book.xlsx: %+v", xlsx)
	}
	if sum.Files[0].As != "image" || sum.Files[3].As != "resource" || sum.Files[3].URI == "" {
		t.Errorf("file notes: %+v", sum.Files)
	}
}

func TestMCPRunFailures(t *testing.T) {
	f := &fakeRunner{ready: true, result: sandbox.Result{ExitCode: 1, Stderr: sandbox.Output{Data: []byte("boom\n")}}}
	h := newTest(t, f, nil)
	if res := callRun(t, h, map[string]any{"language": "bash", "code": "exit 1"}); !res.IsError || !strings.Contains(res.Content[0].Text, "boom") {
		t.Errorf("exit 1: %+v", res)
	}
	f.result = sandbox.Result{ExitCode: 124, TimedOut: true}
	if res := callRun(t, h, map[string]any{"language": "bash", "code": "sleep 9"}); !res.IsError {
		t.Errorf("timeout isn't an error")
	}

	ran := len(f.jobs)
	for name, args := range map[string]any{
		"no arguments":     nil,
		"bad language":     map[string]any{"language": "ruby", "code": "puts 1"},
		"unknown field":    map[string]any{"language": "python", "code": "1", "image": "alpine"},
		"bad base64":       map[string]any{"language": "python", "code": "1", "files": []map[string]any{{"name": "a", "content": "!!"}}},
		"timeout too long": map[string]any{"language": "python", "code": "1", "timeoutSec": 301},
	} {
		res := callRun(t, h, args)
		if !res.IsError || res.Content[0].Text == "" {
			t.Errorf("%s: %+v", name, res)
		}
	}
	if len(f.jobs) != ran {
		t.Errorf("invalid calls ran %d jobs", len(f.jobs)-ran)
	}

	f.ready = false
	if res := callRun(t, h, map[string]any{"language": "python", "code": "1"}); !res.IsError || !strings.Contains(res.Content[0].Text, "isn't ready") {
		t.Errorf("not ready: %+v", res)
	}
}

func TestMCPRunBusy(t *testing.T) {
	f := &fakeRunner{ready: true, block: make(chan struct{}), start: make(chan struct{}, 1)}
	h := newTest(t, f, queue.New(1, 0))
	done := make(chan struct{})
	go func() {
		defer close(done)
		do(h, "POST", "/v1/runs", testToken, map[string]any{"language": "python", "code": "1"})
	}()
	<-f.start
	res := callRun(t, h, map[string]any{"language": "python", "code": "1"})
	if !res.IsError || !strings.Contains(res.Content[0].Text, "busy") {
		t.Errorf("busy: %+v", res)
	}
	close(f.block)
	<-done
}

func TestMCPReplyCap(t *testing.T) {
	chunk := make([]byte, 9<<20)
	f := &fakeRunner{ready: true, result: sandbox.Result{Files: []sandbox.File{
		{Name: "a.bin", Data: chunk}, {Name: "b.bin", Data: chunk}, {Name: "c.bin", Data: chunk}, {Name: "d.txt", Data: []byte("small")},
	}}}
	res := callRun(t, newTest(t, f, nil), map[string]any{"language": "python", "code": "1"})
	var sum struct {
		Files        []mcpFileNote `json:"files"`
		FilesDropped []DroppedFile `json:"filesDropped"`
	}
	_ = json.Unmarshal([]byte(res.Content[0].Text), &sum)
	if len(sum.Files) != 3 || sum.Files[2].Name != "d.txt" || len(sum.FilesDropped) != 1 || sum.FilesDropped[0].Name != "c.bin" {
		t.Errorf("cap: files %+v dropped %+v", sum.Files, sum.FilesDropped)
	}
	if len(res.Content) != 4 {
		t.Errorf("%d content blocks, want 4", len(res.Content))
	}
}
