package api

// POST /mcp speaks the Model Context Protocol over its Streamable HTTP
// transport in the simplest form the spec allows: stateless, one JSON-RPC
// message per request, a JSON reply, no sessions and no server-sent events.
// It offers one tool, run, which is POST /v1/runs with its output files as
// MCP content blocks, so any MCP client can use kandang without code of its
// own.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/zakiego/kandang/apps/server/internal/sandbox"
)

// MCPMaxReplyFileBytes caps the output files one MCP reply carries. A run may
// leave 150 MiB in /work/out, but a reply is built whole in memory (as base64,
// in a JSON-RPC envelope) and an agent's model can't take that much anyway.
// Files past the cap are listed in filesDropped.
const MCPMaxReplyFileBytes = 20 << 20

// MCPProtocolVersions are the MCP revisions kandang speaks, newest first.
// Every one of them allows a stateless server with JSON replies.
var MCPProtocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

// JSON-RPC error codes.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

const mcpInstructions = "Kandang runs Python or bash in a throwaway gVisor sandbox with no network. " +
	"Pass input files base64 encoded; the code finds them in /work/in. " +
	"Whatever the code writes to /work/out comes back: images as image content, other files as embedded resources. " +
	"The sandbox has numpy, pandas, matplotlib, Pillow, openpyxl, pypdf and python-docx, and nothing can be installed. " +
	"Anything that needs the internet or a secret has to be fetched first and passed in as a file. " +
	"matplotlib charts already get a house style (dark background, Inter, a colorblind-checked palette), so don't set a style, colors or fonts unless the user asks for them."

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

var nullID = json.RawMessage("null")

func (s *server) mcp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// No GET stream (the server never sends anything on its own) and no
		// sessions to DELETE.
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST a JSON-RPC message to /mcp")
		return
	}
	// The spec asks servers to check Origin against DNS rebinding. Kandang's
	// clients are servers, which don't send one, so any Origin is refused.
	if r.Header.Get("Origin") != "" {
		writeError(w, http.StatusForbidden, "forbidden_origin", "requests from a browser page aren't accepted")
		return
	}
	if !s.authorize(w, r) {
		return
	}
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !slices.Contains(MCPProtocolVersions, v) {
		writeError(w, http.StatusBadRequest, "unsupported_protocol_version",
			fmt.Sprintf("MCP protocol version %q isn't supported; use one of %s", v, strings.Join(MCPProtocolVersions, ", ")))
		return
	}
	done, ok := s.admitBody(w, r)
	if !ok {
		return
	}
	defer done()

	var req rpcRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		var mbe *http.MaxBytesError
		var ute *json.UnmarshalTypeError
		switch {
		case errors.As(err, &mbe):
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", fmt.Sprintf("request body is over %d bytes", MaxBodyBytes))
		case errors.As(err, &ute) && ute.Value == "array":
			writeRPCError(w, nullID, rpcInvalidRequest, "batches aren't supported; send one message per request")
		default:
			writeRPCError(w, nullID, rpcParseError, "the body isn't a JSON-RPC message: "+err.Error())
		}
		return
	}
	if dec.More() {
		writeRPCError(w, nullID, rpcParseError, "the body has data after the JSON-RPC message")
		return
	}
	if req.JSONRPC != "2.0" {
		writeRPCError(w, nullID, rpcInvalidRequest, `jsonrpc must be "2.0"`)
		return
	}
	// A notification (no id), or a response to a request: kandang never sends
	// requests, so neither needs an answer.
	if len(req.ID) == 0 || req.Method == "" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if !validID(req.ID) {
		writeRPCError(w, nullID, rpcInvalidRequest, "id must be a string or a number")
		return
	}

	switch req.Method {
	case "initialize":
		writeRPCResult(w, req.ID, s.mcpInitialize(req.Params))
	case "ping":
		writeRPCResult(w, req.ID, struct{}{})
	case "tools/list":
		writeRPCResult(w, req.ID, map[string]any{"tools": []any{s.runToolDef()}})
	case "tools/call":
		s.mcpCall(w, r.Context(), req)
	default:
		writeRPCError(w, req.ID, rpcMethodNotFound, "method not found: "+req.Method)
	}
}

// validID reports whether a JSON-RPC id is a string or a number (MCP doesn't
// allow null).
func validID(id json.RawMessage) bool {
	switch c := id[0]; {
	case c == '"', c == '-', c >= '0' && c <= '9':
		return true
	}
	return false
}

type mcpImplementation struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

type mcpInitializeResult struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    map[string]any    `json:"capabilities"`
	ServerInfo      mcpImplementation `json:"serverInfo"`
	Instructions    string            `json:"instructions"`
}

// mcpInitialize answers with the client's protocol version when kandang
// speaks it, and with its newest otherwise; the client then decides.
func (s *server) mcpInitialize(params json.RawMessage) mcpInitializeResult {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)
	version := MCPProtocolVersions[0]
	if slices.Contains(MCPProtocolVersions, p.ProtocolVersion) {
		version = p.ProtocolVersion
	}
	return mcpInitializeResult{
		ProtocolVersion: version,
		Capabilities:    map[string]any{"tools": struct{}{}},
		ServerInfo:      mcpImplementation{Name: "kandang", Title: "Kandang", Version: s.opt.Version},
		Instructions:    mcpInstructions,
	}
}

type mcpToolDef struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations mcpAnnotations  `json:"annotations"`
}

type mcpAnnotations struct {
	Title         string `json:"title"`
	ReadOnlyHint  bool   `json:"readOnlyHint"`
	OpenWorldHint bool   `json:"openWorldHint"`
}

// runToolDef is the run tool. It is read-only in MCP's sense: a run can't
// change anything outside its own sandbox, which is gone when it ends.
func (s *server) runToolDef() mcpToolDef {
	l := s.opt.Limits
	schema := fmt.Sprintf(`{
  "type": "object",
  "properties": {
    "language": {"type": "string", "enum": ["python", "bash"], "description": "python runs the code with python3, bash with bash. The working directory is /work."},
    "code": {"type": "string", "description": "The program, at most %d bytes. Read inputs from /work/in, write files to return to /work/out, print anything else."},
    "files": {
      "type": "array",
      "maxItems": %d,
      "description": "Input files, placed in /work/in. At most %d MiB each and %d MiB in total.",
      "items": {
        "type": "object",
        "properties": {
          "name": {"type": "string", "description": "File name, without a directory."},
          "content": {"type": "string", "description": "The file's bytes, base64 encoded."}
        },
        "required": ["name", "content"],
        "additionalProperties": false
      }
    },
    "timeoutSec": {"type": "number", "minimum": 0.001, "maximum": %g, "description": "Seconds before the run is killed. %g by default."}
  },
  "required": ["language", "code"],
  "additionalProperties": false
}`, sandbox.MaxCodeBytes, sandbox.MaxInputFiles, sandbox.MaxInputFileBytes>>20, sandbox.MaxInputBytes>>20,
		l.MaxTimeout.Seconds(), l.DefaultTimeout.Seconds())
	return mcpToolDef{
		Name:  "run",
		Title: "Run code in a sandbox",
		Description: "Run Python or bash in a fresh gVisor sandbox with no network, 1 CPU and a memory cap, then get back the exit code, stdout, stderr " +
			"and the top-level files the code wrote to /work/out (at most 10). Images come back as image content, other files as embedded resources. " +
			"Use it to compute, analyze data, or make charts, spreadsheets and documents. Python has numpy, pandas, matplotlib, Pillow, openpyxl, pypdf and python-docx. " +
			"matplotlib charts get a house style by default (dark, Inter, colorblind-checked colors): don't set a style, colors or fonts unless asked.",
		InputSchema: json.RawMessage(schema),
		Annotations: mcpAnnotations{Title: "Run code in a sandbox", ReadOnlyHint: true, OpenWorldHint: false},
	}
}

// mcpCall runs tools/call. Problems with the arguments, a busy queue or a
// failed run are tool results with isError, so the model can read them and
// try again; only an unknown tool is a protocol error.
func (s *server) mcpCall(w http.ResponseWriter, ctx context.Context, req rpcRequest) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeRPCError(w, req.ID, rpcInvalidParams, "params aren't valid for tools/call: "+err.Error())
		return
	}
	req.Params = nil // the arguments may hold 50 MiB of files; keep one copy
	if p.Name != "run" {
		writeRPCError(w, req.ID, rpcInvalidParams, fmt.Sprintf("unknown tool %q; the only tool is run", p.Name))
		return
	}
	var args RunRequest
	if len(p.Arguments) > 0 && !bytes.Equal(p.Arguments, nullID) {
		dec := json.NewDecoder(bytes.NewReader(p.Arguments))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&args); err != nil {
			writeRPCResult(w, req.ID, toolError("invalid arguments: "+err.Error()))
			return
		}
	}
	p.Arguments = nil
	if !s.opt.Runner.Status().Ready {
		writeRPCResult(w, req.ID, toolError(notReadyMessage))
		return
	}
	job, verr := ValidateRun(args, s.opt.Limits)
	if verr != nil {
		writeRPCResult(w, req.ID, toolError(verr.message))
		return
	}
	args = RunRequest{}
	job.ID = s.opt.NewID()

	res, release, aerr := s.execute(ctx, job)
	if aerr == errGone {
		return
	}
	if aerr != nil {
		msg := aerr.message
		if aerr.code == "busy" {
			msg = "kandang is busy: every run slot and waiting place is taken. Try again in about 10 seconds."
		}
		writeRPCResult(w, req.ID, toolError(msg))
		return
	}
	defer release()
	defer writeDeadline(w)()
	if err := writeRPCResult(w, req.ID, runToolResult(job.ID, res)); err != nil {
		s.opt.Log.Info("run response not delivered", "run", job.ID, "err", err)
	}
}

type toolResult struct {
	Content []any `json:"content"`
	IsError bool  `json:"isError"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type imageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

type resourceContent struct {
	Type     string           `json:"type"`
	Resource embeddedResource `json:"resource"`
}

type embeddedResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

func toolError(msg string) toolResult {
	return toolResult{Content: []any{textContent{Type: "text", Text: msg}}, IsError: true}
}

// mcpRunSummary is the run tool's text content: the run as JSON, minus the
// file contents, which follow as their own content blocks.
type mcpRunSummary struct {
	runHead
	Files        []mcpFileNote `json:"files"`
	FilesDropped []DroppedFile `json:"filesDropped,omitempty"`
}

// mcpFileNote says what a returned file is and how it came back.
type mcpFileNote struct {
	Name     string `json:"name"`
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	As       string `json:"as"` // "image" or "resource"
	URI      string `json:"uri,omitempty"`
}

// runToolResult turns a run into the run tool's result: a JSON summary first,
// then one content block per output file, in the order they came out. A run
// that failed or timed out is an isError result so the model notices.
func runToolResult(runID string, res sandbox.Result) toolResult {
	sum := mcpRunSummary{
		runHead: runHead{
			ExitCode:        res.ExitCode,
			TimedOut:        res.TimedOut,
			DurationMs:      res.Duration.Milliseconds(),
			PeakMemoryMb:    mb(res.PeakMemoryBytes),
			Stdout:          string(res.Stdout.Data),
			Stderr:          string(res.Stderr.Data),
			StdoutTruncated: res.Stdout.Truncated,
			StderrTruncated: res.Stderr.Truncated,
		},
		Files: []mcpFileNote{},
	}
	var blocks []any
	total := 0
	for i := range res.Files {
		f := &res.Files[i]
		if total+len(f.Data) > MCPMaxReplyFileBytes {
			sum.FilesDropped = append(sum.FilesDropped, DroppedFile{
				Name:   f.Name,
				Reason: fmt.Sprintf("over the %d MiB of files an MCP reply carries; POST /v1/runs returns it", MCPMaxReplyFileBytes>>20),
			})
			continue
		}
		total += len(f.Data)
		mime := mimeOf(f.Name, f.Data)
		note := mcpFileNote{Name: f.Name, Size: len(f.Data), MimeType: mime}
		if modelImage(mime) {
			note.As = "image"
			blocks = append(blocks, imageContent{Type: "image", Data: base64.StdEncoding.EncodeToString(f.Data), MimeType: mime})
		} else {
			note.As = "resource"
			note.URI = "kandang://runs/" + runID + "/out/" + url.PathEscape(f.Name)
			r := embeddedResource{URI: note.URI, MimeType: mime}
			if textual(mime) && utf8.Valid(f.Data) {
				r.Text = string(f.Data)
			} else {
				r.Blob = base64.StdEncoding.EncodeToString(f.Data)
			}
			blocks = append(blocks, resourceContent{Type: "resource", Resource: r})
		}
		sum.Files = append(sum.Files, note)
	}
	for _, d := range res.Dropped {
		sum.FilesDropped = append(sum.FilesDropped, DroppedFile{Name: d.Name, Reason: d.Reason})
	}
	head, err := marshal(sum)
	if err != nil {
		head = []byte(fmt.Sprintf(`{"exitCode":%d}`, res.ExitCode))
	}
	content := append([]any{textContent{Type: "text", Text: string(head)}}, blocks...)
	return toolResult{Content: content, IsError: res.ExitCode != 0 || res.TimedOut}
}

// mimeByExt names the types the sandbox's libraries write. Go's mime package
// reads the host's tables, which differ between machines; this doesn't.
var mimeByExt = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".pdf":  "application/pdf",
	".csv":  "text/csv",
	".tsv":  "text/tab-separated-values",
	".txt":  "text/plain",
	".log":  "text/plain",
	".md":   "text/markdown",
	".html": "text/html",
	".json": "application/json",
	".xml":  "application/xml",
	".yaml": "application/yaml",
	".yml":  "application/yaml",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".zip":  "application/zip",
	".gz":   "application/gzip",
	".tar":  "application/x-tar",
}

// mimeOf is a file's type from its extension, or sniffed from its bytes. An
// image type is only kept when the bytes agree, so a model is never handed a
// broken image.
func mimeOf(name string, data []byte) string {
	sniffed := http.DetectContentType(data)
	if i := strings.IndexByte(sniffed, ';'); i >= 0 {
		sniffed = sniffed[:i]
	}
	m, ok := mimeByExt[strings.ToLower(path.Ext(name))]
	if !ok || (modelImage(m) && m != sniffed) {
		return sniffed
	}
	return m
}

// modelImage reports the image types models take as image content.
func modelImage(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

func textual(mime string) bool {
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	switch mime {
	case "application/json", "application/xml", "application/yaml", "image/svg+xml":
		return true
	}
	return false
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) error {
	return writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	_ = writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func writeRPC(w http.ResponseWriter, resp rpcResponse) error {
	b, err := marshal(resp)
	if err != nil {
		return err
	}
	setJSONHeaders(w)
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(append(b, '\n'))
	return err
}
