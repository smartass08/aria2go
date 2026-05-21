package config

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

var wset = "\r\n\t "

// ParseConf reads aria2.conf format from r and applies options to out.
func ParseConf(r io.Reader, out *Options) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}
		if line[0] == '#' {
			continue
		}
		eqIdx := strings.IndexByte(line, '=')
		if eqIdx < 0 {
			continue
		}
		key := strings.Trim(line[:eqIdx], wset)
		value := strings.Trim(line[eqIdx+1:], wset)
		if key == "" {
			continue
		}
		setter, ok := fieldSetters[key]
		if !ok {
			continue
		}
		if err := setter(out, value); err != nil {
			return fmt.Errorf("config: %s=%s: %w", key, value, err)
		}
		out.markExplicit(key)
	}
	return scanner.Err()
}
