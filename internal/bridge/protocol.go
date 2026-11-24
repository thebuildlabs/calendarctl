// Package bridge implements the length-prefixed JSON IPC protocol used to talk
// to the native Swift EventKit helper. The client writes a 4-byte big-endian
// payload length followed by exactly that many bytes of JSON on stdin; the
// helper writes one newline-delimited JSON response on stdout. Diagnostics go
// to stderr.
package bridge

import "encoding/json"

// Framing limits. The helper rejects a frame whose declared length exceeds
// MaxRequestBytes before it buffers the body; the client caps the response so
// a hostile or broken helper cannot exhaust memory.
const (
	MaxRequestBytes  = 1 << 20  // 1 MiB
	MaxResponseBytes = 16 << 20 // 16 MiB
)

// Request is a command sent to the native helper.
//
// Nonce is a per-process secret delivered out-of-band on an inherited file
// descriptor (see Client.Call). The helper requires it to match the value in
// the request, so commands piped directly into the binary are rejected.
type Request struct {
	Command string         `json:"command"`
	Params  map[string]any `json:"params,omitempty"`
	Nonce   string         `json:"nonce,omitempty"`
}

// WireError is the native helper's error representation.
type WireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// Response is the native helper's reply.
type Response struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data,omitempty"`
	Error    *WireError      `json:"error,omitempty"`
	Warnings []string        `json:"warnings,omitempty"`
}

// Commands understood by the Swift helper.
const (
	CmdPing               = "ping"
	CmdCheckPermissions   = "checkPermissions"
	CmdRequestPermissions = "requestPermissions"
	CmdListCalendars      = "listCalendars"
	CmdGetCalendar        = "getCalendar"
	CmdCreateCalendar     = "createCalendar"
	CmdDeleteCalendar     = "deleteCalendar"
	CmdListEvents         = "listEvents"
	CmdGetEvent           = "getEvent"
	CmdCreateEvent        = "createEvent"
	CmdUpdateEvent        = "updateEvent"
	CmdDeleteEvent        = "deleteEvent"
)
