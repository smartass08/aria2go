// Package ioutilx provides I/O utilities including sync.Pool-based buffer pools.
package ioutilx

import "sync"

// Pool4K is a buffer pool with 4096-byte buffers, suitable for message assembly
// and small I/O operations.
var Pool4K = newPool(4 << 10)

// Pool16K is a buffer pool with 16384-byte buffers, matching the BitTorrent
// block size.
var Pool16K = newPool(16 << 10)

// Pool64K is a buffer pool with 65536-byte buffers, suitable for HTTP body
// chunks and large I/O operations.
var Pool64K = newPool(64 << 10)

// pool is a sync.Pool-based buffer pool that allocates buffers of a fixed size.
type pool struct {
	p    sync.Pool
	size int
}

// newPool creates a new pool with the given buffer size in bytes.
func newPool(size int) *pool {
	p := &pool{size: size}
	p.p.New = func() any {
		buf := make([]byte, size)
		return buf
	}
	return p
}

// Get returns a Buf with zero-length slice and the pool's full capacity.
// If the pool is empty, a new buffer is allocated.
func (p *pool) Get() *Buf {
	b := p.p.Get().([]byte)
	return &Buf{B: b[:0], pool: p}
}

// Buf wraps a pooled byte slice and tracks its origin pool so Free can return
// it safely. The B field is the underlying slice, which may be resliced by
// callers up to its full capacity.
type Buf struct {
	B    []byte
	pool *pool
}

// Bytes returns the underlying byte slice.
func (b *Buf) Bytes() []byte {
	return b.B
}

// Free returns the buffer to its origin pool. It is safe to call multiple
// times; subsequent calls after the first are no-ops.
func (b *Buf) Free() {
	if b.pool == nil {
		return
	}
	b.pool.p.Put(b.B)
	b.pool = nil
	b.B = nil
}
