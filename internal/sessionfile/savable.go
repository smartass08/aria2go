package sessionfile

import "github.com/smartass08/aria2go/internal/core"

// ShouldSave returns true if the download entry should be included in session save.
// Only Waiting, Paused, and Active downloads are saved. Complete, Error, and Removed
// are excluded — matching aria2's behaviour (force-save and save-not-found are
// handled at a higher layer).
func ShouldSave(status core.Status) bool {
	switch status {
	case core.StatusComplete, core.StatusError, core.StatusRemoved:
		return false
	default:
		return true
	}
}

// HasAnyURI returns true if the entry has at least one URI (remaining or spent).
// This mirrors the C++ check: file->getRemainingUris().empty() && file->getSpentUris().empty()
// which skips an entry in writeDownloadResult.
func HasAnyURI(entry Entry) bool {
	return len(entry.URIs) > 0
}

// DedupURIs separates spent URIs from remaining. uris is the full URI list
// (e.g. as loaded from a session file) and spent is the subset known to be spent.
// It returns remaining (untried) URIs and dedupedSpent (the spent URIs).
//
// The deduplication works per aria2's SessionSerializer::writeDownloadResult:
// remaining URIs claim first — spent URIs that also appear in remaining are
// already accounted for by remaining, so they appear only in the remaining slice.
func DedupURIs(uris []string, spent []string) (remaining []string, dedupedSpent []string) {
	spentSet := make(map[string]bool, len(spent))
	for _, u := range spent {
		spentSet[u] = true
	}
	for _, u := range uris {
		if !spentSet[u] {
			remaining = append(remaining, u)
		}
	}
	dedupedSpent = spent
	return
}
