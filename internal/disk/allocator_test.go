package disk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllocatorNone(t *testing.T) {
	a := AllocatorNone{}
	if a.Name() != "none" {
		t.Errorf("Name() = %q, want %q", a.Name(), "none")
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := a.Allocate(f, 1024); err != nil {
		t.Fatalf("Allocate returned error: %v", err)
	}

	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 0 {
		t.Errorf("AllocatorNone changed file size: got %d, want 0", fi.Size())
	}
}

func TestAllocatorTrunc(t *testing.T) {
	a := AllocatorTrunc{}
	if a.Name() != "trunc" {
		t.Errorf("Name() = %q, want %q", a.Name(), "trunc")
	}

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

func TestAllocatorPrealloc(t *testing.T) {
	a := &AllocatorPrealloc{BufSize: 256}
	if a.Name() != "prealloc" {
		t.Errorf("Name() = %q, want %q", a.Name(), "prealloc")
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	const wantSize int64 = 1024
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

	content, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(content)) != wantSize {
		t.Fatalf("read %d bytes, want %d", len(content), wantSize)
	}
	for i, b := range content {
		if b != 0 {
			t.Errorf("byte at offset %d is %d, want 0", i, b)
		}
	}
}

func TestAllocatorPreallocDefaultBufSize(t *testing.T) {
	a := &AllocatorPrealloc{} // BufSize == 0, should default to 4096

	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	const wantSize int64 = 8192
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

func TestAllocatorPreallocZeroSize(t *testing.T) {
	a := &AllocatorPrealloc{BufSize: 256}

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
