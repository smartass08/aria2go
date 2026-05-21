// Package netrc parses .netrc files (netrc(5) format).
//
// Supported tokens: machine, login, password, account, default, macdef.
// Comments start with # and blank lines are ignored.
// Multi-line entries are supported.
package netrc

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// Entry represents a single .netrc entry.
type Entry struct {
	Machine  string
	Login    string
	Password string
	Account  string
}

// DefaultEntry represents the default .netrc entry (declared with "default").
type DefaultEntry struct {
	Login    string
	Password string
	Account  string
}

// Parse reads .netrc format from r and returns parsed entries and the
// optional default entry. Entries are returned in file order; callers
// should iterate and use the first match.
func Parse(r io.Reader) ([]Entry, *DefaultEntry, error) {
	var entries []Entry
	var defaultEntry *DefaultEntry

	scanner := bufio.NewScanner(r)
	lineNo := 0

	state := stateGetToken
	var curEntry Entry
	var haveEntry bool

	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		if raw == "" || raw[0] == '#' {
			continue
		}
		i := 0
		for i < len(raw) {
			for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
				i++
			}
			if i >= len(raw) {
				break
			}
			start := i
			for i < len(raw) && raw[i] != ' ' && raw[i] != '\t' {
				i++
			}
			tok := raw[start:i]
			switch state {
			case stateGetToken:
				switch tok {
				case "machine":
					if haveEntry {
						entries = append(entries, curEntry)
						curEntry = Entry{}
					}
					haveEntry = true
					state = stateSetMachine
				case "default":
					if haveEntry {
						entries = append(entries, curEntry)
						curEntry = Entry{}
					}
					haveEntry = true
					curEntry.Machine = ""
					state = stateGetToken
				case "login":
					if !haveEntry {
						return nil, nil, fmt.Errorf("netrc:%d: login token without preceding machine or default", lineNo)
					}
					state = stateSetLogin
				case "password":
					if !haveEntry {
						return nil, nil, fmt.Errorf("netrc:%d: password token without preceding machine or default", lineNo)
					}
					state = stateSetPassword
				case "account":
					if !haveEntry {
						return nil, nil, fmt.Errorf("netrc:%d: account token without preceding machine or default", lineNo)
					}
					state = stateSetAccount
				case "macdef":
					if !haveEntry {
						return nil, nil, fmt.Errorf("netrc:%d: macdef token without preceding machine or default", lineNo)
					}
					state = stateSetMacdef
				default:
					return nil, nil, fmt.Errorf("netrc:%d: %q encountered where 'machine' or 'default' expected", lineNo, tok)
				}
			case stateSetMachine:
				curEntry.Machine = tok
				state = stateGetToken
			case stateSetLogin:
				curEntry.Login = tok
				state = stateGetToken
			case stateSetPassword:
				curEntry.Password = tok
				state = stateGetToken
			case stateSetAccount:
				curEntry.Account = tok
				state = stateGetToken
			case stateSetMacdef:
				if err := skipMacdef(scanner, &lineNo); err != nil {
					return nil, nil, fmt.Errorf("netrc:%d: macdef I/O error: %w", lineNo, err)
				}
				state = stateGetToken
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("netrc: read error: %w", err)
	}
	if state != stateGetToken {
		return nil, nil, fmt.Errorf("netrc:%d: EOF reached where a token expected", lineNo)
	}

	if haveEntry {
		if curEntry.Machine != "" {
			entries = append(entries, curEntry)
		} else {
			defaultEntry = &DefaultEntry{
				Login:    curEntry.Login,
				Password: curEntry.Password,
				Account:  curEntry.Account,
			}
		}
	}

	return entries, defaultEntry, nil
}

type parseState int

const (
	stateGetToken parseState = iota
	stateSetMachine
	stateSetLogin
	stateSetPassword
	stateSetAccount
	stateSetMacdef
)

func skipMacdef(scanner *bufio.Scanner, lineNo *int) error {
	for scanner.Scan() {
		*lineNo++
		line := scanner.Text()
		if line == "" {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// LoadDefault reads ~/.netrc from the user's home directory.
// It returns the parsed entries and optional default entry, or an error.
// If the file does not exist, it returns an empty slice and no default.
func LoadDefault() ([]Entry, *DefaultEntry, error) {
	home, err := defaultHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("netrc: cannot determine home directory: %w", err)
	}
	f, err := os.Open(filepath.Join(home, ".netrc"))
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil, nil
		}
		return nil, nil, fmt.Errorf("netrc: cannot open .netrc: %w", err)
	}
	defer f.Close()
	return Parse(f)
}

func defaultHomeDir() (string, error) {
	if home, ok := os.LookupEnv("HOME"); ok {
		return home, nil
	}
	if runtime.GOOS == "windows" {
		if home, ok := os.LookupEnv("USERPROFILE"); ok {
			return home, nil
		}
		if drive, ok := os.LookupEnv("HOMEDRIVE"); ok {
			if path, pathOK := os.LookupEnv("HOMEPATH"); pathOK {
				return drive + path, nil
			}
		}
	}
	return os.UserHomeDir()
}
