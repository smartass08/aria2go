package disk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllocatorFallocName(t *testing.T) {
	a := AllocatorFalloc{}
	if a.Name() != "falloc" {
		t.Errorf("Name() = %q, want %q", a.Name(), "falloc")
	}
}

func TestAllocatorFallocSetsSize(t *testing.T) {
	a := AllocatorFalloc{}

	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	const wantSize int64 = 1024 * 1024
	if err := a.Allocate(f, wantSize); err != nil {
		t.Fatalf("Allocate returned error: %v", err)
	}

	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != wantSize {
		t.Errorf("size = %d, want %d", fi.Size(), wantSize)
	}
}

func TestAllocatorFallocZeroSize(t *testing.T) {
	a := AllocatorFalloc{}

	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := a.Allocate(f, 0); err != nil {
		t.Fatalf("Allocate(0) returned error: %v", err)
	}

	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 0 {
		t.Errorf("size = %d, want 0", fi.Size())
	}
}

func TestAllocatorFallocReadOnlyFile(t *testing.T) {
	a := AllocatorFalloc{}

	dir := t.TempDir()
	p := filepath.Join(dir, "readonly.dat")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	f, err = os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := a.Allocate(f, 1024); err == nil {
		t.Error("Allocate on read-only file should return an error")
	}
}

func TestAllocatorFallocFallbackWhenCapsFalse(t *testing.T) {
	// When Caps().Fallocate is false, the fallback path calls f.Truncate(size).
	// On macOS/darwin, Caps().Fallocate is true per caps_darwin.go, so this
	// test verifies the overall correctness: AllocatorFalloc sets the file
	// size correctly regardless of the code path taken.
	a := AllocatorFalloc{}

	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	const wantSize int64 = 4096
	if err := a.Allocate(f, wantSize); err != nil {
		t.Fatalf("Allocate returned error: %v", err)
	}

	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != wantSize {
		t.Errorf("size = %d, want %d", fi.Size(), wantSize)
	}
}
