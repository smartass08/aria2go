package platform

import (
	"runtime"
	"testing"
)

func TestCaps_MatchesCurrentOS(t *testing.T) {
	c := Caps()

	switch runtime.GOOS {
	case "linux":
		if !c.Fallocate {
			t.Error("linux: Fallocate should be true")
		}
		if !c.MMapAnon {
			t.Error("linux: MMapAnon should be true")
		}
		if !c.InterfaceBind {
			t.Error("linux: InterfaceBind should be true")
		}
		if !c.UnixSocket {
			t.Error("linux: UnixSocket should be true")
		}
		if !c.Signals {
			t.Error("linux: Signals should be true")
		}
		if c.Pagesize != 4096 {
			t.Errorf("linux: Pagesize should be 4096, got %d", c.Pagesize)
		}
	case "darwin":
		if !c.Fallocate {
			t.Error("darwin: Fallocate should be true")
		}
		if !c.MMapAnon {
			t.Error("darwin: MMapAnon should be true")
		}
		if c.InterfaceBind {
			t.Error("darwin: InterfaceBind should be false")
		}
		if !c.UnixSocket {
			t.Error("darwin: UnixSocket should be true")
		}
		if !c.Signals {
			t.Error("darwin: Signals should be true")
		}
		if c.Pagesize != 16384 {
			t.Errorf("darwin: Pagesize should be 16384, got %d", c.Pagesize)
		}
	case "freebsd":
		if !c.Fallocate {
			t.Error("freebsd: Fallocate should be true")
		}
		if !c.MMapAnon {
			t.Error("freebsd: MMapAnon should be true")
		}
		if !c.InterfaceBind {
			t.Error("freebsd: InterfaceBind should be true")
		}
		if !c.UnixSocket {
			t.Error("freebsd: UnixSocket should be true")
		}
		if !c.Signals {
			t.Error("freebsd: Signals should be true")
		}
		if c.Pagesize != 4096 {
			t.Errorf("freebsd: Pagesize should be 4096, got %d", c.Pagesize)
		}
	case "openbsd":
		if c.Fallocate {
			t.Error("openbsd: Fallocate should be false")
		}
		if !c.MMapAnon {
			t.Error("openbsd: MMapAnon should be true")
		}
		if !c.InterfaceBind {
			t.Error("openbsd: InterfaceBind should be true")
		}
		if !c.UnixSocket {
			t.Error("openbsd: UnixSocket should be true")
		}
		if !c.Signals {
			t.Error("openbsd: Signals should be true")
		}
		if c.Pagesize != 4096 {
			t.Errorf("openbsd: Pagesize should be 4096, got %d", c.Pagesize)
		}
	case "windows":
		if !c.Fallocate {
			t.Error("windows: Fallocate should be true")
		}
		if !c.MMapAnon {
			t.Error("windows: MMapAnon should be true")
		}
		if c.InterfaceBind {
			t.Error("windows: InterfaceBind should be false")
		}
		if c.UnixSocket {
			t.Error("windows: UnixSocket should be false")
		}
		if c.Signals {
			t.Error("windows: Signals should be false")
		}
		if c.Pagesize != 4096 {
			t.Errorf("windows: Pagesize should be 4096, got %d", c.Pagesize)
		}
	default:
		t.Logf("unsupported OS %q in test; verifying cap defaults", runtime.GOOS)
	}
}

func TestCaps_Idempotent(t *testing.T) {
	c1 := Caps()
	c2 := Caps()
	if c1 != c2 {
		t.Error("Caps() returned different values across calls")
	}
}

func TestCaps_NonZero(t *testing.T) {
	c := Caps()
	if c.Pagesize <= 0 {
		t.Errorf("Pagesize must be positive, got %d", c.Pagesize)
	}
}
