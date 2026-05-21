package engine

import (
	"sync"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
)

type stoppedRing struct {
	mu             sync.Mutex
	buf            []*downloadResult
	head           int
	size           int
	index          map[core.GID]int
	evictedTotal   int
	evictedErrors  int
	evictedLastErr core.ErrorCode
}

func newStoppedRing(capacity int) *stoppedRing {
	if capacity <= 0 {
		capacity = 1000
	}
	return &stoppedRing{
		buf:   make([]*downloadResult, capacity),
		index: make(map[core.GID]int, capacity),
	}
}

func (r *stoppedRing) push(dr *downloadResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pushLocked(dr)
}

func (r *stoppedRing) pushLocked(dr *downloadResult) {
	capacity := len(r.buf)
	if r.size >= capacity {
		evicted := r.buf[r.head]
		delete(r.index, evicted.gid)
		r.evictedTotal++
		if evicted.belongsTo == 0 && evicted.errCode != core.ExitSuccess {
			r.evictedErrors++
			r.evictedLastErr = evicted.errCode
		}
	}
	pos := (r.head + r.size) % capacity
	r.buf[pos] = dr
	r.index[dr.gid] = pos
	if r.size >= capacity {
		r.head = (r.head + 1) % capacity
	} else {
		r.size++
	}
}

func (r *stoppedRing) get(offset, num int) []*downloadResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	if offset >= r.size {
		return nil
	}
	end := offset + num
	if end > r.size {
		end = r.size
	}

	capacity := len(r.buf)
	result := make([]*downloadResult, end-offset)
	for i := 0; i < end-offset; i++ {
		pos := (r.head + offset + i) % capacity
		result[i] = r.buf[pos]
	}
	return result
}

func (r *stoppedRing) getByGID(gid core.GID) (*downloadResult, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	pos, ok := r.index[gid]
	if !ok {
		return nil, false
	}
	return r.buf[pos], true
}

func (r *stoppedRing) optionsByGID(gid core.GID) (*config.Options, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	pos, ok := r.index[gid]
	if !ok {
		return nil, false
	}
	opts := r.buf[pos].opts
	if opts == nil {
		return &config.Options{}, true
	}
	cp := *opts
	return &cp, true
}

func (r *stoppedRing) mergeOptions(gid core.GID, opts *config.Options) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	pos, ok := r.index[gid]
	if !ok {
		return false
	}
	dr := r.buf[pos]
	if dr.state == core.StatusRemoved {
		return false
	}
	dr.opts = config.Merge(dr.opts, opts)
	return true
}

func (r *stoppedRing) removeByGID(gid core.GID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.index[gid]; !ok {
		return false
	}
	kept := make([]*downloadResult, 0, r.size-1)
	for i := 0; i < r.size; i++ {
		pos := (r.head + i) % len(r.buf)
		dr := r.buf[pos]
		if dr.gid != gid {
			kept = append(kept, dr)
		}
		r.buf[pos] = nil
	}
	r.head = 0
	r.size = 0
	r.index = make(map[core.GID]int, len(r.buf))
	for _, dr := range kept {
		r.pushLocked(dr)
	}
	return true
}

func (r *stoppedRing) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

func (r *stoppedRing) purge() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := 0; i < r.size; i++ {
		pos := (r.head + i) % len(r.buf)
		r.buf[pos] = nil
	}
	r.head = 0
	r.size = 0
	r.index = make(map[core.GID]int, len(r.buf))
}

func (r *stoppedRing) evictionInfo() (total, errors int, lastErr core.ErrorCode) {
	return r.evictedTotal, r.evictedErrors, r.evictedLastErr
}

func (r *stoppedRing) snapshotStatuses(offset, num int) []Status {
	r.mu.Lock()
	defer r.mu.Unlock()

	if offset >= r.size {
		return nil
	}
	end := offset + num
	if end > r.size {
		end = r.size
	}

	capacity := len(r.buf)
	result := make([]Status, 0, end-offset)
	for i := 0; i < end-offset; i++ {
		pos := (r.head + offset + i) % capacity
		dr := r.buf[pos]
		result = append(result, Status{
			GID:          dr.gid,
			Status:       dr.state,
			ErrorCode:    dr.errCode,
			ErrorMessage: dr.errMsg,
			BelongsTo:    dr.belongsTo,
			Following:    dr.following,
			FollowedBy:   dr.followedBy,
		})
	}
	return result
}
