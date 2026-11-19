// Package api is kandang's HTTP surface: GET /health and POST /v1/runs. It
// validates every request against kandang's own schema, so a caller can only
// pick a language, the code, input files and a timeout; never an image,
// mounts, devices, capabilities or runtime flags.
package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/zakiego/kandang/apps/server/internal/queue"
	"github.com/zakiego/kandang/apps/server/internal/sandbox"
)

// MaxBodyBytes caps a request body before any decoding: 50 MiB of files as
// base64 (4/3), 100 KiB of code even if every byte is JSON-escaped (\u0000 is
// 6 bytes), and room for names and field syntax.
const MaxBodyBytes = sandbox.MaxInputBytes/3*4 + sandbox.MaxCodeBytes*6 + 1<<20

// Options wires the handler to its parts.
type Options struct {
	Runner  sandbox.Runner
	Queue   *queue.Queue
	Token   []byte
	Version string
	Limits  sandbox.Limits
	Log     *slog.Logger
	NewID   func() string // run ids; sandbox.NewID when nil
}

type server struct {
	opt       Options
	tokenHash [32]byte
}

// New returns the HTTP handler.
func New(opt Options) http.Handler {
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	if opt.NewID == nil {
		opt.NewID = sandbox.NewID
	}
	s := &server{opt: opt, tokenHash: sha256.Sum256(opt.Token)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /v1/runs", s.run)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	return mux
}

type healthResponse struct {
	OK      bool        `json:"ok"`
	Version string      `json:"version"`
	Runsc   string      `json:"runsc"`
	Rootfs  string      `json:"rootfs"`
	Queue   queue.Stats `json:"queue"`
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	st := s.opt.Runner.Status()
	code := http.StatusOK
	if !st.Ready {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, healthResponse{
		OK: st.Ready, Version: s.opt.Version, Runsc: st.Runsc, Rootfs: st.Rootfs, Queue: s.opt.Queue.Stats(),
	})
}

// authorized compares the bearer token in constant time. Both sides are
// hashed first so the comparison doesn't leak the token's length either.
func (s *server) authorized(r *http.Request) bool {
	if len(s.opt.Token) == 0 {
		return false
	}
	h := r.Header.Get("Authorization")
	scheme, tok, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	got := sha256.Sum256([]byte(strings.TrimSpace(tok)))
	return subtle.ConstantTimeCompare(got[:], s.tokenHash[:]) == 1
}

// RunRequest is the body of POST /v1/runs.
type RunRequest struct {
	Language   string      `json:"language"`
	Code       *string     `json:"code"`
	Files      []InputFile `json:"files"`
	TimeoutSec *float64    `json:"timeoutSec"`
}

// InputFile is one input, its content base64 encoded.
type InputFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// RunResponse is what a finished run returns. writeRun produces exactly this
// shape without ever holding it whole.
type RunResponse struct {
	ExitCode        int           `json:"exitCode"`
	TimedOut        bool          `json:"timedOut"`
	DurationMs      int64         `json:"durationMs"`
	PeakMemoryMb    float64       `json:"peakMemoryMb"`
	Stdout          string        `json:"stdout"`
	Stderr          string        `json:"stderr"`
	StdoutTruncated bool          `json:"stdoutTruncated"`
	StderrTruncated bool          `json:"stderrTruncated"`
	Files           []OutputFile  `json:"files"`
	FilesDropped    []DroppedFile `json:"filesDropped,omitempty"`
}

// OutputFile is one file from /work/out, its content base64 encoded.
type OutputFile struct {
	Name    string `json:"name"`
	Size    int    `json:"size"`
	Content string `json:"content"`
}

// DroppedFile is an output that didn't come back, with the reason.
type DroppedFile struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// ErrorResponse is every error body.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// apiError is a validation failure with its HTTP status.
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.message }

func bad(code, format string, a ...any) *apiError {
	return &apiError{status: http.StatusBadRequest, code: code, message: fmt.Sprintf(format, a...)}
}

func tooLarge(code, format string, a ...any) *apiError {
	return &apiError{status: http.StatusRequestEntityTooLarge, code: code, message: fmt.Sprintf(format, a...)}
}

func (s *server) run(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="kandang"`)
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or wrong bearer token")
		return
	}
	if !s.opt.Runner.Status().Ready {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "the sandbox isn't ready (runsc or the rootfs is missing); see GET /health")
		return
	}
	if r.ContentLength > MaxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", fmt.Sprintf("request body is over %d bytes", MaxBodyBytes))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	var req RunRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", fmt.Sprintf("request body is over %d bytes", MaxBodyBytes))
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "request body isn't valid JSON for this endpoint: "+err.Error())
		return
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body has data after the JSON object")
		return
	}
	job, verr := ValidateRun(req, s.opt.Limits)
	if verr != nil {
		writeError(w, verr.status, verr.code, verr.message)
		return
	}
	job.ID = s.opt.NewID()

	release, err := s.opt.Queue.Acquire(r.Context())
	if errors.Is(err, queue.ErrBusy) {
		w.Header().Set("Retry-After", "10")
		writeError(w, http.StatusTooManyRequests, "busy", "every run slot and waiting place is taken; try again shortly")
		return
	}
	if err != nil {
		return // the client went away while waiting
	}
	defer release()

	start := time.Now()
	res, err := s.opt.Runner.Run(r.Context(), job)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			s.opt.Log.Info("run canceled", "run", job.ID, "err", err)
			return
		case errors.Is(err, sandbox.ErrNotReady), errors.Is(err, sandbox.ErrUnsupported):
			writeError(w, http.StatusServiceUnavailable, "not_ready", err.Error())
		default:
			s.opt.Log.Error("run failed", "run", job.ID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "the run failed inside kandang; run id "+job.ID)
		}
		return
	}
	s.opt.Log.Info("run",
		"run", job.ID, "language", job.Language, "exit", res.ExitCode, "timedOut", res.TimedOut,
		"oom", res.OOMKilled, "ms", res.Duration.Milliseconds(), "peakMb", mb(res.PeakMemoryBytes),
		"filesIn", len(job.Files), "filesOut", len(res.Files), "dropped", len(res.Dropped),
		"wallMs", time.Since(start).Milliseconds())
	if err := writeRun(w, res); err != nil {
		s.opt.Log.Info("run response not delivered", "run", job.ID, "err", err)
	}
}

// ValidateRun checks a request and turns it into a job (without an id).
func ValidateRun(req RunRequest, l sandbox.Limits) (sandbox.Job, *apiError) {
	var job sandbox.Job
	switch req.Language {
	case sandbox.LangPython, sandbox.LangBash:
		job.Language = req.Language
	case "":
		return job, bad("invalid_language", `language is required: "python" or "bash"`)
	default:
		return job, bad("invalid_language", `language must be "python" or "bash"`)
	}
	if req.Code == nil {
		return job, bad("invalid_code", "code is required")
	}
	if strings.TrimSpace(*req.Code) == "" {
		return job, bad("invalid_code", "code is empty")
	}
	if len(*req.Code) > sandbox.MaxCodeBytes {
		return job, tooLarge("code_too_large", "code is %d bytes; the limit is %d", len(*req.Code), sandbox.MaxCodeBytes)
	}
	if strings.IndexByte(*req.Code, 0) >= 0 {
		return job, bad("invalid_code", "code contains a NUL byte")
	}
	job.Code = []byte(*req.Code)

	job.Timeout = l.DefaultTimeout
	if req.TimeoutSec != nil {
		t := *req.TimeoutSec
		max := l.MaxTimeout.Seconds()
		if math.IsNaN(t) || t <= 0 || t > max {
			return job, bad("invalid_timeout", "timeoutSec must be more than 0 and at most %g", max)
		}
		job.Timeout = time.Duration(t * float64(time.Second))
	}

	if len(req.Files) > sandbox.MaxInputFiles {
		return job, bad("too_many_files", "%d input files; the limit is %d", len(req.Files), sandbox.MaxInputFiles)
	}
	seen := map[string]bool{}
	total := 0
	for i, f := range req.Files {
		name, err := sandbox.CleanName(f.Name)
		if err != nil {
			return job, bad("invalid_file", "files[%d].name: %v", i, err)
		}
		if seen[name] {
			return job, bad("invalid_file", "files[%d].name: %q is already used by another file", i, name)
		}
		seen[name] = true
		if base64.StdEncoding.DecodedLen(len(f.Content)) > sandbox.MaxInputFileBytes+2 {
			return job, tooLarge("file_too_large", "files[%d] (%s) is over %d bytes", i, name, sandbox.MaxInputFileBytes)
		}
		data, err := base64.StdEncoding.DecodeString(f.Content)
		if err != nil {
			return job, bad("invalid_file", "files[%d].content isn't valid base64: %v", i, err)
		}
		if len(data) > sandbox.MaxInputFileBytes {
			return job, tooLarge("file_too_large", "files[%d] (%s) is %d bytes; the limit is %d", i, name, len(data), sandbox.MaxInputFileBytes)
		}
		total += len(data)
		if total > sandbox.MaxInputBytes {
			return job, tooLarge("files_too_large", "input files add up to more than %d bytes", sandbox.MaxInputBytes)
		}
		job.Files = append(job.Files, sandbox.File{Name: name, Data: data})
	}
	return job, nil
}

// runHead is a run's response without its files: the part that is small.
type runHead struct {
	ExitCode        int     `json:"exitCode"`
	TimedOut        bool    `json:"timedOut"`
	DurationMs      int64   `json:"durationMs"`
	PeakMemoryMb    float64 `json:"peakMemoryMb"`
	Stdout          string  `json:"stdout"`
	Stderr          string  `json:"stderr"`
	StdoutTruncated bool    `json:"stdoutTruncated"`
	StderrTruncated bool    `json:"stderrTruncated"`
}

// writeRun streams a RunResponse. A run can return 150 MiB of files, and the
// code inside the sandbox decides that. Building the JSON in memory held it
// three times over (the data, its base64 and the encoder's buffer), enough
// for one run to push kandang past its memory cap. Each file is encoded
// straight to the client instead and let go once it is out.
//
// It takes res.Files over: their data is released as it is written.
func writeRun(w http.ResponseWriter, res sandbox.Result) error {
	head, err := marshal(runHead{
		ExitCode:        res.ExitCode,
		TimedOut:        res.TimedOut,
		DurationMs:      res.Duration.Milliseconds(),
		PeakMemoryMb:    mb(res.PeakMemoryBytes),
		Stdout:          string(res.Stdout.Data),
		Stderr:          string(res.Stderr.Data),
		StdoutTruncated: res.Stdout.Truncated,
		StderrTruncated: res.Stderr.Truncated,
	})
	if err != nil {
		return err
	}
	var dropped []byte
	if len(res.Dropped) > 0 {
		list := make([]DroppedFile, 0, len(res.Dropped))
		for _, d := range res.Dropped {
			list = append(list, DroppedFile{Name: d.Name, Reason: d.Reason})
		}
		if dropped, err = marshal(list); err != nil {
			return err
		}
	}
	setJSONHeaders(w)
	w.WriteHeader(http.StatusOK)
	bw := bufio.NewWriterSize(w, 64<<10)
	bw.Write(head[:len(head)-1]) // without its closing brace
	bw.WriteString(`,"files":[`)
	for i := range res.Files {
		f := &res.Files[i]
		name, err := marshal(f.Name)
		if err != nil {
			return err
		}
		if i > 0 {
			bw.WriteByte(',')
		}
		fmt.Fprintf(bw, `{"name":%s,"size":%d,"content":"`, name, len(f.Data))
		enc := base64.NewEncoder(base64.StdEncoding, bw)
		enc.Write(f.Data)
		enc.Close()
		bw.WriteString(`"}`)
		f.Data = nil
		if err := bw.Flush(); err != nil {
			return err // the client went away; stop encoding for nobody
		}
	}
	bw.WriteByte(']')
	if dropped != nil {
		bw.WriteString(`,"filesDropped":`)
		bw.Write(dropped)
	}
	bw.WriteString("}\n")
	return bw.Flush()
}

// marshal is json.Marshal without HTML escaping and without the newline.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// mb is bytes in MiB, rounded to one decimal.
func mb(b int64) float64 {
	return math.Round(float64(b)/(1<<20)*10) / 10
}

func setJSONHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	setJSONHeaders(w)
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{Error: code, Message: message})
}
