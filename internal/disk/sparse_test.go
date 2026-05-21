package disk

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSparseAllocator_Name(t *testing.T) {
	s := NewSparseAllocator(AllocatorTrunc{})
	if s.Name() != "sparse" {
		t.Errorf("Name() = %q, want %q", s.Name(), "sparse")
	}
}

func TestSparseAllocator_Allocate_Size(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.dat")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	const size int64 = 1024 * 1024
	s := NewSparseAllocator(AllocatorTrunc{})
	if err := s.Allocate(f, size); err != nil {
		t.Fatalf("Allocate failed: %v", err)
	}

	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != size {
		t.Errorf("file size = %d, want %d", fi.Size(), size)
	}
}

func TestSparseAllocator_Allocate_DelegatesToInner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.dat")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	const size int64 = 4096
	s := NewSparseAllocator(&AllocatorPrealloc{BufSize: 256})
	if err := s.Allocate(f, size); err != nil {
		t.Fatalf("Allocate failed: %v", err)
	}

	buf := make([]byte, size)
	n, err := f.ReadAt(buf, 0)
	if err != nil {
		t.Fatal(err)
	}
	if int64(n) != size {
		t.Fatalf("read %d bytes, want %d", n, size)
	}
	for i, b := range buf {
		if b != 0 {
			t.Errorf("byte at offset %d = %d, want 0 (Prealloc inner allocator should zero-fill)", i, b)
			break
		}
	}
}

func TestSparseAllocator_Allocate_ZeroSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.dat")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	s := NewSparseAllocator(AllocatorTrunc{})
	if err := s.Allocate(f, 0); err != nil {
		t.Fatalf("Allocate(0) failed: %v", err)
	}

	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 0 {
		t.Errorf("file size = %d, want 0", fi.Size())
	}
}

func TestIsSparseSupported(t *testing.T) {
	got := IsSparseSupported()
	if runtime.GOOS == "windows" {
		if got {
			t.Error("IsSparseSupported() = true on Windows, want false")
		}
	} else {
		if !got {
			t.Errorf("IsSparseSupported() = false on %s, want true", runtime.GOOS)
		}
	}
}
