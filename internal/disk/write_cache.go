package disk

import (
	"os"
	"sync"
)

// WriteCacheEntry holds cached write data for one file region.
type WriteCacheEntry struct {
	goff int64  // offset in file
	data []byte // cached data (nil means no entry / not set)
}

// WriteCache provides LRU write caching across multiple files.
// It coalesces contiguous writes into per-file buffers and evicts
// the least-recently-used entry when total cached data exceeds maxSize.
type WriteCache struct {
	mu        sync.Mutex
	entries   map[*os.File]*WriteCacheEntry
	maxSize   int64
	totalSize int64
	lru       []*os.File
}

// NewWriteCache creates a write cache with the given maximum total
// cached size in bytes.
func NewWriteCache(maxSize int64) *WriteCache {
	return &WriteCache{
		entries: make(map[*os.File]*WriteCacheEntry),
		maxSize: maxSize,
	}
}

// Add caches data for f at the given offset. If the data is contiguous
// with the existing cached entry, it is appended in-place. Otherwise
// the existing entry is flushed to disk and a new entry is created.
//
// After adding, if totalSize exceeds maxSize, the least-recently-used
// entries are flushed until totalSize is within the limit.
func (wc *WriteCache) Add(f *os.File, offset int64, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	wc.mu.Lock()
	defer wc.mu.Unlock()

	entry, ok := wc.entries[f]
	if ok && entry.data != nil {
		if entry.goff+int64(len(entry.data)) == offset {
			entry.data = append(entry.data, data...)
			wc.totalSize += int64(len(data))
			wc.touchLRULocked(f)
			wc.evictLocked()
			return nil
		}
		if err := wc.flushEntryLocked(f); err != nil {
			return err
		}
	}

	buf := make([]byte, len(data))
	copy(buf, data)
	entry = &WriteCacheEntry{
		goff: offset,
		data: buf,
	}
	wc.entries[f] = entry
	wc.totalSize += int64(len(data))
	wc.touchLRULocked(f)
	wc.evictLocked()
	return nil
}

// Flush writes the cached data for f to disk and removes the entry
// from the cache. If f has no cached entry, Flush returns nil.
func (wc *WriteCache) Flush(f *os.File) error {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	return wc.flushEntryLocked(f)
}

func (wc *WriteCache) flushEntryLocked(f *os.File) error {
	entry, ok := wc.entries[f]
	if !ok || entry.data == nil {
		return nil
	}
	_, err := f.WriteAt(entry.data, entry.goff)
	if err != nil {
		return Wrap("write", f.Name(), err)
	}
	wc.totalSize -= int64(len(entry.data))
	delete(wc.entries, f)
	wc.removeFromLRULocked(f)
	return nil
}

// Close flushes all cached entries to disk. After Close returns, the
// cache is empty and may be reused.
func (wc *WriteCache) Close() error {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	for len(wc.lru) > 0 {
		f := wc.lru[0]
		if err := wc.flushEntryLocked(f); err != nil {
			return err
		}
	}
	return nil
}

func (wc *WriteCache) touchLRULocked(f *os.File) {
	wc.removeFromLRULocked(f)
	wc.lru = append(wc.lru, f)
}

func (wc *WriteCache) removeFromLRULocked(f *os.File) {
	for i, lf := range wc.lru {
		if lf == f {
			wc.lru = append(wc.lru[:i], wc.lru[i+1:]...)
			return
		}
	}
}

func (wc *WriteCache) evictLocked() {
	for wc.totalSize > wc.maxSize && len(wc.lru) > 0 {
		f := wc.lru[0]
		_ = wc.flushEntryLocked(f)
	}
}
