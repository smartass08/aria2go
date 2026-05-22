package nullfs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNullfsMount(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only linux uses FUSE; darwin is a passthrough")
	}
	if !canUseFuse(t) {
		t.Skip("cannot use FUSE (missing permissions)")
	}

	parent := filepath.Join(os.TempDir(), "nullfs-test-"+t.Name())
	os.RemoveAll(parent)
	m, err := New(parent, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer m.Unmount()

	target := filepath.Join(m.Path, "hello.bin")
	out, err := exec.Command("sh", "-c", "dd if=/dev/zero of="+target+" bs=1024 count=1 2>&1").CombinedOutput()
	if err != nil {
		t.Fatalf("child write: %v (%s)", err, out)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size() != 0 {
		t.Errorf("stat size=%d, want=0", st.Size())
	}
}

func canUseFuse(t *testing.T) bool {
	t.Helper()
	if _, err := os.Stat("/dev/fuse"); err != nil {
		return false
	}
	return true
}
