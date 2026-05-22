package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestHelp(t *testing.T) {
	out, _ := exec.Command("go", "run", ".", "-help").CombinedOutput()
	if !strings.Contains(string(out), "Usage") {
		t.Errorf("no usage in output: %s", out)
	}
	if !strings.Contains(string(out), "-aria2c") {
		t.Errorf("missing -aria2c flag in output: %s", out)
	}
	if !strings.Contains(string(out), "-aria2go") {
		t.Errorf("missing -aria2go flag in output: %s", out)
	}
}
