package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	ce "github.com/thebuildlabs/calendarctl/internal/errors"
	"github.com/thebuildlabs/calendarctl/internal/logging"
	"github.com/thebuildlabs/calendarctl/internal/text"
)

// handshakeBytes is the size of the per-process secret shared with the helper.
const handshakeBytes = 32

// Client invokes the native helper process for each request.
type Client struct {
	Path    string
	Timeout time.Duration
	Log     *logging.Logger

	nonce   string
	authErr error

	// resolution is set by NewResolved. When present, each call re-validates
	// the helper immediately before exec and holds mu across validation and
	// process start, closing the resolve→exec TOCTOU window.
	resolution *Resolution
	mu         sync.Mutex
}

// AppBundlePath returns the .app bundle containing a helper binary, if any.
func AppBundlePath(binaryPath string) string {
	if binaryPath == "" {
		return ""
	}
	dir := filepath.Dir(binaryPath)
	for i := 0; i < 8; i++ {
		if filepath.Ext(dir) == ".app" {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// New constructs a client for a path the caller has already validated. It does
// not validate the path itself; production callers should use NewResolved so
// each invocation is re-validated before execution.
func New(path string, log *logging.Logger) *Client {
	if log == nil {
		log = logging.Noop()
	}
	c := &Client{Path: path, Timeout: 45 * time.Second, Log: log}
	var seed [handshakeBytes]byte
	if _, err := rand.Read(seed[:]); err != nil {
		// Do not fall back to a predictable token: fail the handshake instead.
		c.authErr = fmt.Errorf("generate bridge handshake: %w", err)
	} else {
		c.nonce = hex.EncodeToString(seed[:])
	}
	return c
}

// NewResolved constructs a client from a validated Resolution. Every Call
// re-checks ownership, signature and file identity before starting the helper.
func NewResolved(r *Resolution, log *logging.Logger) *Client {
	c := New(r.Path, log)
	c.resolution = r
	return c
}

// frameRequest returns a 4-byte big-endian length prefix followed by the JSON
// payload, matching the helper's length-prefixed reader.
func frameRequest(payload []byte) []byte {
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)
	return frame
}

// limitedBuffer bounds the bytes accepted from a helper response.
type limitedBuffer struct {
	bytes.Buffer
	limit int
	err   error
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if b.Len()+len(p) > b.limit {
		b.err = fmt.Errorf("bridge response exceeds %d bytes", b.limit)
		return 0, b.err
	}
	return b.Buffer.Write(p)
}

// Call sends a length-prefixed request and decodes the newline-delimited
// response.
func (c *Client) Call(ctx context.Context, command string, params map[string]any, out any) error {
	if c.Path == "" {
		return ce.Unavailable("bridge", "EventKit bridge path is empty")
	}
	if c.authErr != nil {
		return ce.Wrap(ce.CodeUnavailable, "bridge.handshake", c.authErr)
	}
	payload, err := json.Marshal(Request{Command: command, Params: params, Nonce: c.nonce})
	if err != nil {
		return ce.Wrap(ce.CodeGeneral, "bridge.encode", err)
	}
	if len(payload) > MaxRequestBytes {
		return ce.InvalidInput("bridge.encode", fmt.Sprintf("request exceeds %d bytes", MaxRequestBytes))
	}
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	// The nonce travels on an inherited pipe (fd 3 in the child), not in the
	// JSON request alone. The helper compares both before running a command.
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return ce.Wrap(ce.CodeUnavailable, "bridge.handshake", err)
	}
	defer readEnd.Close()
	if _, err := writeEnd.WriteString(c.nonce); err != nil {
		writeEnd.Close()
		return ce.Wrap(ce.CodeUnavailable, "bridge.handshake", err)
	}
	writeEnd.Close()

	stdout := &limitedBuffer{limit: MaxResponseBytes}
	var stderr bytes.Buffer
	started := time.Now()
	err = c.runHelper(ctx, frameRequest(payload), readEnd, stdout, &stderr)
	c.Log.Debug("bridge request", logging.F("command", command), logging.F("ms", time.Since(started).Milliseconds()))
	if stdout.err != nil {
		return ce.Wrap(ce.CodeGeneral, "bridge.decode", stdout.err)
	}
	if err != nil {
		if ctx.Err() != nil {
			return ce.Unavailable("bridge", "EventKit bridge timed out or was interrupted")
		}
		detail := cleanDiagnostic(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return ce.Unavailable("bridge", fmt.Sprintf("EventKit bridge failed: %s", detail))
	}
	if diagnostic := cleanDiagnostic(stderr.String()); diagnostic != "" {
		c.Log.Warn("bridge diagnostic", logging.F("command", command), logging.F("bytes", len(diagnostic)))
	}

	var resp Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return ce.Wrap(ce.CodeGeneral, "bridge.decode", fmt.Errorf("invalid bridge response: %w", err))
	}
	if !resp.OK || resp.Error != nil {
		if resp.Error == nil {
			return ce.New(ce.CodeGeneral, "bridge", "native helper returned an error")
		}
		return mapWireError(resp.Error)
	}
	if out == nil || len(resp.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return ce.Wrap(ce.CodeGeneral, "bridge.decode", fmt.Errorf("decode bridge data: %w", err))
	}
	return nil
}

// runHelper re-validates the resolved helper and starts it. Validation and
// process start are serialized and performed back-to-back so a same-user
// process cannot swap the binary in between.
func (c *Client) runHelper(ctx context.Context, frame []byte, readEnd *os.File, stdout, stderr io.Writer) error {
	if c.resolution != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := c.resolution.Revalidate(); err != nil {
			return ce.Unavailable("bridge", fmt.Sprintf("bridge helper failed revalidation: %v", err))
		}
	}
	cmd := exec.CommandContext(ctx, c.Path)
	cmd.ExtraFiles = []*os.File{readEnd}
	cmd.Stdin = bytes.NewReader(frame)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// cleanDiagnostic normalizes helper stderr before it is shown to the user.
// A hostile or misconfigured helper must not be able to inject terminal
// control sequences or dump unbounded text into an error message.
func cleanDiagnostic(s string) string {
	s = strings.Join(strings.Fields(text.SanitizeControl(s)), " ")
	runes := []rune(s)
	const max = 512
	if len(runes) > max {
		s = string(runes[:max]) + "…"
	}
	return s
}

func mapWireError(e *WireError) error {
	code := ce.CodeGeneral
	switch strings.ToLower(e.Code) {
	case "permission_denied", "not_authorized", "denied", "restricted", "permission_prompt_unavailable", "unauthorized":
		code = ce.CodePermission
	case "not_found":
		code = ce.CodeNotFound
	case "invalid_input", "bad_request":
		code = ce.CodeInvalidInput
	case "already_exists", "duplicate":
		code = ce.CodeAlreadyExist
	case "unavailable", "no_calendar_store":
		code = ce.CodeUnavailable
	}
	return &ce.Error{Code: code, Op: "bridge", Message: e.Message, Hint: e.Hint}
}
