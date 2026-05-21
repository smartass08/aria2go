// Package platform provides per-OS capability detection and syscall glue.
// Higher-level packages query platform.Caps() instead of using build tags directly.
package platform

import "sync"

// Cap describes OS-level features available on the current platform.
// Fields are constant for the process lifetime.
type Cap struct {
	Fallocate     bool
	MMapAnon      bool
	InterfaceBind bool
	UnixSocket    bool
	Signals       bool
	Pagesize      int
}

var (
	capsOnce sync.Once
	capsVal  Cap
)

// Caps returns the compile-time capability set for the current platform.
// The result is computed once on first call and cached for subsequent lookups.
func Caps() Cap {
	capsOnce.Do(func() {
		capsVal = capsInit()
	})
	return capsVal
}
