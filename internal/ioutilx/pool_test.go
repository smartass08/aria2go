package ioutilx

import (
	"sync"
	"testing"
)

func TestPool4K_Capacity(t *testing.T) {
	buf := Pool4K.Get()
	if got := cap(buf.Bytes()); got != 4<<10 {
		t.Fatalf("Pool4K.Get() capacity = %d, want %d", got, 4<<10)
	}
	buf.Free()
}

func TestPool16K_Capacity(t *testing.T) {
	buf := Pool16K.Get()
	if got := cap(buf.Bytes()); got != 16<<10 {
		t.Fatalf("Pool16K.Get() capacity = %d, want %d", got, 16<<10)
	}
	buf.Free()
}

func TestPool64K_Capacity(t *testing.T) {
	buf := Pool64K.Get()
	if got := cap(buf.Bytes()); got != 64<<10 {
		t.Fatalf("Pool64K.Get() capacity = %d, want %d", got, 64<<10)
	}
	buf.Free()
}

func TestBuf_GetFree_RoundTrip(t *testing.T) {
	buf := Pool4K.Get()

	// Use the buffer — write some data and reslice.
	b := buf.Bytes()
	b = b[:10]
	copy(b, "0123456789")
	if cap(b) != 4<<10 {
		t.Fatalf("after reslice, capacity = %d, want %d", cap(b), 4<<10)
	}
	buf.B = b

	buf.Free()

	// Get another buffer from the same pool; it should have the full capacity.
	buf2 := Pool4K.Get()
	if got := cap(buf2.Bytes()); got != 4<<10 {
		t.Fatalf("round-trip Get() capacity = %d, want %d", got, 4<<10)
	}
	buf2.Free()
}

func TestBuf_DoubleFree(t *testing.T) {
	buf := Pool4K.Get()
	buf.Free()
	// Second Free must not panic.
	buf.Free()
}

func TestBuf_Bytes_AfterFree(t *testing.T) {
	buf := Pool4K.Get()
	buf.Free()
	if buf.Bytes() != nil {
		t.Fatal("Bytes() after Free() should return nil")
	}
}

func TestBuf_ConcurrentGetFree(t *testing.T) {
	const (
		goroutines = 64
		iterations = 1000
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				buf := Pool16K.Get()
				if got := cap(buf.Bytes()); got != 16<<10 {
					t.Errorf("capacity = %d, want %d", got, 16<<10)
					return
				}
				// Write data to simulate real usage.
				b := buf.Bytes()
				b = b[:16]
				copy(b, "concurrent-test!!")
				buf.B = b
				buf.Free()
			}
		}()
	}

	wg.Wait()
}

func TestBuf_MultiplePools(t *testing.T) {
	pools := []*pool{Pool4K, Pool16K, Pool64K}
	sizes := []int{4 << 10, 16 << 10, 64 << 10}

	for i, p := range pools {
		buf := p.Get()
		if got := cap(buf.Bytes()); got != sizes[i] {
			t.Errorf("pool[%d] capacity = %d, want %d", i, got, sizes[i])
		}
		buf.Free()
	}
}

func TestBuf_ZeroLengthSlice(t *testing.T) {
	buf := Pool4K.Get()
	if len(buf.Bytes()) != 0 {
		t.Fatalf("Get() should return zero-length slice, got len=%d", len(buf.Bytes()))
	}
	buf.Free()
}

func TestNewPool_ReturnsNonNil(t *testing.T) {
	for _, size := range []int{4 << 10, 16 << 10, 64 << 10} {
		p := newPool(size)
		if p == nil {
			t.Fatalf("newPool(%d) returned nil", size)
		}
		if p.size != size {
			t.Fatalf("newPool(%d).size = %d", size, p.size)
		}
	}
}
