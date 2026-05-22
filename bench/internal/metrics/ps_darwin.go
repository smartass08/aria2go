//go:build darwin

package metrics

import (
	"fmt"
	"os/exec"
)

func execPS(pid int) (string, error) {
	out, err := exec.Command("ps", "-p", fmt.Sprint(pid), "-o", "rss=,vsz=,pcpu=").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
