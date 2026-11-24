// Package logging implements deliberately minimal structured logging.
//
// Privacy rule: log only identifiers, counts, durations and status codes.
// Event titles, notes, locations, attendees and calendar names are private
// data and must never be passed to the logger.
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	LevelError Level = iota
	LevelWarn
	LevelInfo
	LevelDebug
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	default:
		return "error"
	}
}

func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	default:
		return LevelError
	}
}

// Field is a single structured key/value pair.
type Field struct {
	Key   string
	Value any
}

func F(key string, value any) Field { return Field{Key: key, Value: value} }

type Logger struct {
	mu    sync.Mutex
	out   io.Writer
	level Level
	json  bool
	base  []Field
}

func New(out io.Writer, level Level, asJSON bool) *Logger {
	if out == nil {
		out = os.Stderr
	}
	return &Logger{out: out, level: level, json: asJSON}
}

// Noop returns a logger that discards everything.
func Noop() *Logger { return New(io.Discard, LevelError, false) }

func (l *Logger) Enabled(level Level) bool { return level <= l.level }

func (l *Logger) log(level Level, msg string, fields ...Field) {
	if !l.Enabled(level) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	if l.json {
		obj := map[string]any{"ts": ts, "level": level.String(), "msg": msg}
		for _, f := range l.base {
			obj[f.Key] = f.Value
		}
		for _, f := range fields {
			obj[f.Key] = f.Value
		}
		b, err := json.Marshal(obj)
		if err == nil {
			fmt.Fprintln(l.out, string(b))
		}
		return
	}

	var b strings.Builder
	b.WriteString(ts)
	b.WriteByte(' ')
	b.WriteString(strings.ToUpper(level.String()))
	b.WriteByte(' ')
	b.WriteString(msg)
	for _, f := range l.base {
		fmt.Fprintf(&b, " %s=%v", f.Key, f.Value)
	}
	for _, f := range fields {
		fmt.Fprintf(&b, " %s=%v", f.Key, f.Value)
	}
	fmt.Fprintln(l.out, b.String())
}

func (l *Logger) Debug(msg string, fields ...Field) { l.log(LevelDebug, msg, fields...) }
func (l *Logger) Info(msg string, fields ...Field)  { l.log(LevelInfo, msg, fields...) }
func (l *Logger) Warn(msg string, fields ...Field)  { l.log(LevelWarn, msg, fields...) }
func (l *Logger) Error(msg string, fields ...Field) { l.log(LevelError, msg, fields...) }

// With returns a child logger that always includes the given fields.
func (l *Logger) With(fields ...Field) *Logger {
	base := make([]Field, 0, len(l.base)+len(fields))
	base = append(base, l.base...)
	base = append(base, fields...)
	return &Logger{out: l.out, level: l.level, json: l.json, base: base}
}
