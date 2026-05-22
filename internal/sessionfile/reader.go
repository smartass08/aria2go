package sessionfile

import (
	"io"
	"sync"

	"github.com/smartass08/aria2go/internal/core"
)

// Approximate number of entries to pre-allocate per call.
const readPrealloc = 64

// entryPool reuses Entry objects for reading to reduce GC pressure.
var entryPool = sync.Pool{
	New: func() interface{} { return &Entry{} },
}

// Read parses entries from an aria2 session file. It transparently handles
// both plain text and gzip-compressed input (auto-detected via magic bytes).
func Read(r io.Reader) ([]Entry, error) {
	parser, err := NewParser(r)
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	entries := make([]Entry, 0, readPrealloc)
	for {
		entry, ok, err := parser.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		currentEntry := entryPool.Get().(*Entry)
		*currentEntry = entry
		entries = append(entries, *currentEntry)
		entryPool.Put(currentEntry)
	}

	return entries, nil
}

func finalizeEntry(e *Entry, uris []string, opts map[string]string, unknown map[string]string, unknownOrder []OptionLine) {
	e.URIs = uris
	if opts == nil {
		opts = make(map[string]string)
	}
	e.Options = opts
	if unknown == nil {
		unknown = make(map[string]string)
	}
	e.Unknown = unknown
	if len(unknownOrder) > 0 {
		e.UnknownOrder = append(e.UnknownOrder[:0], unknownOrder...)
	} else {
		e.UnknownOrder = nil
	}

	if gidStr, ok := opts["gid"]; ok {
		gid, err := core.ParseGID(gidStr)
		if err == nil {
			e.GID = gid
		}
	}

	if pauseVal, ok := opts["pause"]; ok && pauseVal == "true" {
		e.Status = core.StatusPaused
	} else {
		e.Status = core.StatusWaiting
	}
}
