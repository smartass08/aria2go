package sessionfile

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"
)

const parserBufferSize = 1024 * 1024

type lineReader struct {
	scanner *bufio.Scanner
	closers []io.Closer
}

func newLineReader(r io.Reader, closers ...io.Closer) (*lineReader, error) {
	br := bufio.NewReader(r)
	magic, err := br.Peek(2)
	if err != nil && err != io.EOF {
		return nil, err
	}

	reader := io.Reader(br)
	if len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gzr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("sessionfile: gzip reader: %w", err)
		}
		reader = gzr
		closers = append(closers, gzr)
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), parserBufferSize)
	return &lineReader{scanner: scanner, closers: closers}, nil
}

func (r *lineReader) next() (string, bool, error) {
	if r.scanner.Scan() {
		return r.scanner.Text(), true, nil
	}
	if err := r.scanner.Err(); err != nil {
		return "", false, err
	}
	return "", false, nil
}

func (r *lineReader) close() error {
	var firstErr error
	for i := len(r.closers) - 1; i >= 0; i-- {
		if err := r.closers[i].Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Parser incrementally reads aria2 input/session entries from an input stream.
// It mirrors aria2's UriListParser behavior: blank lines and comment lines are
// skipped, option lines must begin with whitespace, and malformed entry
// options are left for higher-level config parsing.
type Parser struct {
	reader    *lineReader
	pending   string
	hasPend   bool
	exhausted bool
}

func NewParser(r io.Reader) (*Parser, error) {
	reader, err := newLineReader(r)
	if err != nil {
		return nil, err
	}
	return &Parser{reader: reader}, nil
}

func OpenParser(path string) (*Parser, error) {
	if path == "-" {
		reader, err := newLineReader(os.Stdin)
		if err != nil {
			return nil, err
		}
		return &Parser{reader: reader}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	reader, err := newLineReader(f, f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Parser{reader: reader}, nil
}

func (p *Parser) Close() error {
	if p.reader == nil {
		return nil
	}
	err := p.reader.close()
	p.reader = nil
	return err
}

func (p *Parser) HasNext() bool {
	return !p.exhausted || p.hasPend
}

func (p *Parser) Next() (Entry, bool, error) {
	if p.reader == nil {
		return Entry{}, false, nil
	}

	for {
		line, ok, err := p.readLine()
		if err != nil {
			return Entry{}, false, fmt.Errorf("sessionfile: read: %w", err)
		}
		if !ok {
			p.exhausted = true
			return Entry{}, false, nil
		}
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			continue
		}

		entry := Entry{}
		uris := strings.Split(line, "\t")
		if len(uris) > 1 && uris[len(uris)-1] == "" {
			uris = uris[:len(uris)-1]
		}

		var opts map[string]string
		var unknown map[string]string
		var unknownOrder []OptionLine

		for {
			nextLine, ok, err := p.readLine()
			if err != nil {
				return Entry{}, false, fmt.Errorf("sessionfile: read: %w", err)
			}
			if !ok {
				p.exhausted = true
				finalizeEntry(&entry, uris, opts, unknown, unknownOrder)
				return entry, true, nil
			}
			if nextLine == "" || nextLine[0] == '#' {
				continue
			}
			if nextLine[0] != ' ' && nextLine[0] != '\t' {
				p.pending = nextLine
				p.hasPend = true
				finalizeEntry(&entry, uris, opts, unknown, unknownOrder)
				return entry, true, nil
			}

			key, val, ok := parseOptionLine(nextLine)
			if !ok {
				continue
			}

			if knownKeys[key] {
				if opts == nil {
					opts = make(map[string]string, 4)
				}
				if cumulativeKeys[key] {
					if existing, ok := opts[key]; ok {
						opts[key] = existing + "\n" + val
					} else {
						opts[key] = val
					}
				} else {
					opts[key] = val
				}
				continue
			}

			if unknown == nil {
				unknown = make(map[string]string, 4)
			}
			unknownOrder = append(unknownOrder, OptionLine{Key: key, Value: val})
			if existing, ok := unknown[key]; ok {
				unknown[key] = existing + "\n" + val
			} else {
				unknown[key] = val
			}
		}
	}
}

func (p *Parser) readLine() (string, bool, error) {
	if p.hasPend {
		line := p.pending
		p.pending = ""
		p.hasPend = false
		return line, true, nil
	}
	return p.reader.next()
}

func parseOptionLine(line string) (key, value string, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return "", "", false
	}
	eqIdx := strings.IndexByte(trimmed, '=')
	if eqIdx < 0 {
		return "", "", false
	}
	key = trimmed[:eqIdx]
	if key == "" {
		return "", "", false
	}
	return key, trimmed[eqIdx+1:], true
}
