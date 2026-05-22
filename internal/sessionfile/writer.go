package sessionfile

import (
	"compress/gzip"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/smartass08/aria2go/internal/core"
)

var gzipWriterPool = sync.Pool{
	New: func() interface{} { return gzip.NewWriter(io.Discard) },
}

// Write serializes entries to w in aria2 session file format.
// The output is plain text (no gzip compression).
func Write(w io.Writer, entries []Entry) error {
	return writeEntries(w, entries)
}

// WriteGzip serializes entries to w in gzip-compressed aria2 session file format.
func WriteGzip(w io.Writer, entries []Entry) error {
	gw := gzipWriterPool.Get().(*gzip.Writer)
	gw.Reset(w)
	err := writeEntries(gw, entries)
	if err != nil {
		gzipWriterPool.Put(gw)
		return err
	}
	if err := gw.Close(); err != nil {
		gzipWriterPool.Put(gw)
		return err
	}
	gzipWriterPool.Put(gw)
	return nil
}

func writeEntries(w io.Writer, entries []Entry) error {
	for i := range entries {
		if err := writeEntry(w, &entries[i]); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(w io.Writer, e *Entry) error {
	if len(e.URIs) == 0 {
		return nil
	}

	for _, uri := range e.URIs {
		if _, err := io.WriteString(w, uri); err != nil {
			return fmt.Errorf("sessionfile: write URI: %w", err)
		}
		if _, err := w.Write([]byte{'\t'}); err != nil {
			return fmt.Errorf("sessionfile: write URI separator: %w", err)
		}
	}
	if _, err := w.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("sessionfile: write newline after URIs: %w", err)
	}

	// Special-first-key 1: gid (always)
	if err := writeOptionLine(w, "gid", e.GID.Hex()); err != nil {
		return err
	}

	// Special-first-key 2: pause (only if paused)
	if e.Status == core.StatusPaused {
		if err := writeOptionLine(w, "pause", "true"); err != nil {
			return err
		}
	}

	// Collect all keys to emit in canonical order.
	emitKeys := make(map[string]bool, len(e.Options)+len(e.Unknown))
	for k := range e.Options {
		emitKeys[k] = true
	}
	for k := range e.Unknown {
		emitKeys[k] = true
	}

	// Emit known keys in canonical order.
	written := map[string]bool{
		"gid":   true,
		"pause": true,
	}
	for _, key := range canonicalKeyOrder {
		if !emitKeys[key] {
			continue
		}
		if written[key] {
			continue
		}
		val, inOpts := e.Options[key]
		if !inOpts {
			continue
		}
		written[key] = true

		if cumulativeKeys[key] {
			for _, line := range splitOptionValues(val) {
				if err := writeOptionLine(w, key, line); err != nil {
					return err
				}
			}
		} else {
			if err := writeOptionLine(w, key, val); err != nil {
				return err
			}
		}
	}

	// Emit unknown keys (sorted for determinism).
	if len(e.UnknownOrder) > 0 {
		writeUnknownOrder := true
		grouped := make(map[string][]string, len(e.Unknown))
		for _, line := range e.UnknownOrder {
			grouped[line.Key] = append(grouped[line.Key], line.Value)
		}
		for key, lines := range grouped {
			if e.Unknown[key] != strings.Join(lines, "\n") {
				writeUnknownOrder = false
				break
			}
		}
		if writeUnknownOrder {
			for _, line := range e.UnknownOrder {
				if written[line.Key] {
					continue
				}
				if _, ok := e.Unknown[line.Key]; !ok {
					continue
				}
				if err := writeOptionLine(w, line.Key, line.Value); err != nil {
					return err
				}
			}
			for key := range grouped {
				written[key] = true
			}
		}
	}

	unknownKeys := make([]string, 0, len(e.Unknown))
	for k := range e.Unknown {
		if !written[k] {
			unknownKeys = append(unknownKeys, k)
		}
	}
	sort.Strings(unknownKeys)
	for _, key := range unknownKeys {
		for _, line := range splitPreservedValues(e.Unknown[key]) {
			if err := writeOptionLine(w, key, line); err != nil {
				return err
			}
		}
	}

	return nil
}

func writeOptionLine(w io.Writer, key, val string) error {
	if _, err := io.WriteString(w, " "); err != nil {
		return fmt.Errorf("sessionfile: write option %s: %w", key, err)
	}
	if _, err := io.WriteString(w, key); err != nil {
		return fmt.Errorf("sessionfile: write option %s: %w", key, err)
	}
	if _, err := io.WriteString(w, "="); err != nil {
		return fmt.Errorf("sessionfile: write option %s: %w", key, err)
	}
	if _, err := io.WriteString(w, val); err != nil {
		return fmt.Errorf("sessionfile: write option %s: %w", key, err)
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("sessionfile: write option %s: %w", key, err)
	}
	return nil
}

// SerializedHash returns the SHA-1 digest of the exact plain-text session
// serialization. Callers can compare it with a previous digest to skip
// unchanged saves before choosing plain or gzip output.
func SerializedHash(entries []Entry) ([sha1.Size]byte, error) {
	h := sha1.New()
	if err := writeEntries(h, entries); err != nil {
		return [sha1.Size]byte{}, err
	}
	var digest [sha1.Size]byte
	copy(digest[:], h.Sum(nil))
	return digest, nil
}

func splitOptionValues(val string) []string {
	if val == "" {
		return nil
	}
	raw := strings.Split(val, "\n")
	values := raw[:0]
	for _, line := range raw {
		if line != "" {
			values = append(values, line)
		}
	}
	return values
}

func splitPreservedValues(val string) []string {
	return strings.Split(val, "\n")
}

// AtomicSave writes entries to path atomically using a temp-file-and-rename
// strategy. If gzip is true or path ends with ".gz", the output is
// gzip-compressed.
func AtomicSave(path string, entries []Entry, useGzip bool) error {
	if !useGzip {
		useGzip = strings.HasSuffix(path, ".gz")
	}
	tmpPath := path + "__temp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("sessionfile: create temp: %w", err)
	}
	defer os.Remove(tmpPath)

	var w io.Writer = f
	var gw *gzip.Writer
	if useGzip {
		gw = gzipWriterPool.Get().(*gzip.Writer)
		gw.Reset(f)
		w = gw
	}

	if err := writeEntries(w, entries); err != nil {
		f.Close()
		if gw != nil {
			gzipWriterPool.Put(gw)
		}
		return err
	}

	if gw != nil {
		if err := gw.Close(); err != nil {
			f.Close()
			gzipWriterPool.Put(gw)
			return fmt.Errorf("sessionfile: gzip close: %w", err)
		}
		gzipWriterPool.Put(gw)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sessionfile: sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("sessionfile: close temp: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("sessionfile: rename: %w", err)
	}
	return nil
}
