//go:build linux

package metrics

import "fmt"

func execPS(_ int) (string, error) {
	return "", fmt.Errorf("not used on linux")
}
