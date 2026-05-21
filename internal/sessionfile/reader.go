package sessionfile

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
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
	// Peek at first two bytes to detect gzip magic.
	br := bufio.NewReader(r)
	magic, err := br.Peek(2)
	if err != nil && err != io.EOF {
		br = bufio.NewReader(r)
	}

	var scanner *bufio.Scanner
	if len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gzr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("sessionfile: gzip reader: %w", err)
		}
		defer gzr.Close()
		scanner = bufio.NewScanner(gzr)
	} else {
		scanner = bufio.NewScanner(br)
	}

	entries := make([]Entry, 0, readPrealloc)
	var currentEntry *Entry
	var currentURIs []string
	var currentOptions map[string]string
	var currentUnknown map[string]string
	var currentUnknownOrder []OptionLine

	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" || line[0] == '#' {
			continue
		}

		trimmed := strings.TrimLeft(line, " \t")
		if trimmed != line {
			kv := trimmed
			eqIdx := strings.IndexByte(kv, '=')
			if eqIdx < 0 {
				continue
			}
			key := kv[:eqIdx]
			val := kv[eqIdx+1:]

			if key == "" {
				continue
			}

			if currentEntry == nil {
				continue
			}

			if knownKeys[key] {
				if currentOptions == nil {
					currentOptions = make(map[string]string, 4)
				}
				if cumulativeKeys[key] {
					if existing, ok := currentOptions[key]; ok {
						currentOptions[key] = existing + "\n" + val
					} else {
						currentOptions[key] = val
					}
				} else {
					currentOptions[key] = val
				}
			} else {
				if currentUnknown == nil {
					currentUnknown = make(map[string]string, 4)
				}
				currentUnknownOrder = append(currentUnknownOrder, OptionLine{Key: key, Value: val})
				if existing, ok := currentUnknown[key]; ok {
					currentUnknown[key] = existing + "\n" + val
				} else {
					currentUnknown[key] = val
				}
			}
		} else {
			if currentEntry != nil {
				finalizeEntry(currentEntry, currentURIs, currentOptions, currentUnknown, currentUnknownOrder)
				entries = append(entries, *currentEntry)
				entryPool.Put(currentEntry)
			}

			uris := strings.Split(line, "\t")
			if len(uris) > 1 && uris[len(uris)-1] == "" {
				uris = uris[:len(uris)-1]
			}
			currentEntry = entryPool.Get().(*Entry)
			*currentEntry = Entry{}
			currentURIs = uris
			currentOptions = nil
			currentUnknown = nil
			currentUnknownOrder = nil
		}
	}

	if currentEntry != nil {
		finalizeEntry(currentEntry, currentURIs, currentOptions, currentUnknown, currentUnknownOrder)
		entries = append(entries, *currentEntry)
		entryPool.Put(currentEntry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("sessionfile: read: %w", err)
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
