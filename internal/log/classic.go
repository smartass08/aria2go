package log

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"

	"github.com/smartass08/aria2go/internal/ioutilx"
)

// ClassicHandler is a slog.Handler that outputs aria2-style log lines.
//
// Output format (file):
//
//	YYYY-MM-DD HH:MM:SS.uuuuuu [LEVEL] [sourceFile:lineNum] message key=value ...\n
//
// Timestamps use microsecond precision, matching aria2's %06ld tv_usec format.
//
// ClassicHandler is safe for concurrent use.
type ClassicHandler struct {
	w        io.Writer
	mu       sync.Mutex
	opts     slog.HandlerOptions
	minLevel Level

	preAttrs []slog.Attr
	group    string
}

// NewClassicHandler creates a ClassicHandler that writes to w.
// If opts is nil, default slog.HandlerOptions are used.
func NewClassicHandler(w io.Writer, opts *slog.HandlerOptions) *ClassicHandler {
	h := &ClassicHandler{
		w:        w,
		minLevel: LevelNotice,
	}
	if opts != nil {
		h.opts = *opts
		if l, ok := opts.Level.(Level); ok {
			h.minLevel = l
		} else {
			h.minLevel = Level(opts.Level.Level())
		}
	}
	return h
}

// Enabled reports whether the handler emits records at the given level.
// Uses the handler's raw aria2 level ordering (DEBUG < INFO < NOTICE < WARN < ERROR),
// matching aria2's fileLogEnabled/consoleLogEnabled comparison.
func (h *ClassicHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return int(level) >= int(h.minLevel)
}

// Handle formats and writes a single log record.
func (h *ClassicHandler) Handle(ctx context.Context, r slog.Record) error {
	pooled := ioutilx.Pool4K.Get()
	buf := bytes.NewBuffer(pooled.Bytes())
	defer func() {
		pooled.Free()
	}()

	// YYYY-MM-DD HH:MM:SS.uuuuuu — stack buffer avoids AppendFormat alloc (exactly 26 bytes)
	var ts [26]byte
	buf.Write(r.Time.AppendFormat(ts[:0], "2006-01-02 15:04:05.000000"))
	buf.WriteByte(' ')
	// [LEVEL]
	buf.WriteByte('[')
	buf.WriteString(levelDisplayName(r.Level))
	buf.WriteByte(']')
	// [sourceFile:lineNum] — strconv.AppendInt into stack buffer avoids allocation
	if r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		if f.File != "" {
			buf.WriteString(" [")
			buf.WriteString(filepath.Base(f.File))
			buf.WriteByte(':')
			var lineBuf [20]byte
			buf.Write(strconv.AppendInt(lineBuf[:0], int64(f.Line), 10))
			buf.WriteByte(']')
		}
	}
	buf.WriteByte(' ')
	// message
	buf.WriteString(r.Message)

	// pre-attached attrs (from WithAttrs)
	for _, a := range h.preAttrs {
		h.appendAttr(buf, a)
	}
	// record attrs (record groups are resolved by slog's Attrs method)
	r.Attrs(func(a slog.Attr) bool {
		h.appendAttr(buf, a)
		return true
	})

	buf.WriteByte('\n')

	h.mu.Lock()
	_, err := h.w.Write(buf.Bytes())
	h.mu.Unlock()
	return err
}

// WithAttrs returns a new Handler whose attributes include both
// the receiver's attributes and attrs.
func (h *ClassicHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	h2 := h.clone()
	h2.preAttrs = append(h2.preAttrs, attrs...)
	return h2
}

// WithGroup returns a new Handler with the given group appended to
// the receiver's existing groups.
func (h *ClassicHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := h.clone()
	if h2.group != "" {
		h2.group = h2.group + "." + name
	} else {
		h2.group = name
	}
	return h2
}

func (h *ClassicHandler) clone() *ClassicHandler {
	h2 := &ClassicHandler{
		w:        h.w,
		opts:     h.opts,
		minLevel: h.minLevel,
		preAttrs: make([]slog.Attr, len(h.preAttrs)),
		group:    h.group,
	}
	copy(h2.preAttrs, h.preAttrs)
	return h2
}

func (h *ClassicHandler) appendAttr(buf *bytes.Buffer, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}
	buf.WriteByte(' ')
	if h.group != "" {
		buf.WriteString(h.group)
		buf.WriteByte('.')
	}
	buf.WriteString(a.Key)
	buf.WriteByte('=')

	v := a.Value
	switch v.Kind() {
	case slog.KindString:
		buf.WriteString(v.String())
	case slog.KindInt64:
		var n [20]byte
		buf.Write(strconv.AppendInt(n[:0], v.Int64(), 10))
	case slog.KindUint64:
		var n [20]byte
		buf.Write(strconv.AppendUint(n[:0], v.Uint64(), 10))
	case slog.KindBool:
		if v.Bool() {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	default:
		buf.WriteString(v.String())
	}
}

// levelDisplayName returns the aria2-style uppercase level name.
func levelDisplayName(l slog.Level) string {
	switch {
	case l == slog.LevelDebug:
		return "DEBUG"
	case l == slog.LevelInfo:
		return "INFO"
	case l == slog.Level(LevelNotice):
		return "NOTICE"
	case l == slog.LevelWarn:
		return "WARN"
	case l == slog.LevelError:
		return "ERROR"
	default:
		return "NOTICE"
	}
}
