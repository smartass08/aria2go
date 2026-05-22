package tracker

import (
	"reflect"
	"testing"
)

func TestNormalizeAnnounceTiersAppliesExcludeAndAdditionalTrackers(t *testing.T) {
	base := [][]string{
		{"udp://t1:6969/announce", "http://t2/announce"},
		{"http://t3/announce"},
	}

	got := NormalizeAnnounceTiers("", base, []string{"udp://t1:6969/announce,http://missing/announce"}, []string{"udp://extra:80/announce", "http://extra2/announce"})
	want := [][]string{
		{"http://t2/announce"},
		{"http://t3/announce"},
		{"udp://extra:80/announce"},
		{"http://extra2/announce"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeAnnounceTiers() = %#v, want %#v", got, want)
	}
}

func TestNormalizeAnnounceTiersWildcardExcludeClearsTorrentTrackersOnly(t *testing.T) {
	got := NormalizeAnnounceTiers("http://torrent/announce", nil, []string{"*"}, []string{"udp://extra:6969/announce"})
	want := [][]string{{"udp://extra:6969/announce"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeAnnounceTiers() = %#v, want %#v", got, want)
	}
}

func TestAnnounceListSuccessAdvancesEventAndPromotesTracker(t *testing.T) {
	list := NewAnnounceList([][]string{{"http://a", "http://b"}})
	list.AnnounceFailure()
	if got := list.GetAnnounce(); got != "http://b" {
		t.Fatalf("after failure current announce = %q, want http://b", got)
	}

	list.AnnounceSuccess()

	if got := list.GetAnnounce(); got != "http://b" {
		t.Fatalf("after success current announce = %q, want http://b", got)
	}
	if got := list.GetEvent(); got != AnnounceDownloading {
		t.Fatalf("event = %v, want %v", got, AnnounceDownloading)
	}
}

func TestAnnounceListMoveToStoppedAllowedTier(t *testing.T) {
	list := NewAnnounceList([][]string{{"http://a"}, {"http://b"}})
	if list.CountStoppedAllowedTier() != 0 {
		t.Fatalf("CountStoppedAllowedTier() = %d, want 0 before started announce succeeds", list.CountStoppedAllowedTier())
	}

	list.AnnounceSuccess()
	if !list.CurrentTierAcceptsStoppedEvent() {
		t.Fatal("current tier should accept stopped after successful started announce")
	}

	list.AnnounceFailure()
	if list.AllTiersFailed() {
		t.Fatal("expected second tier to remain available after first tier failure")
	}
	list.AnnounceFailure()
	if !list.AllTiersFailed() {
		t.Fatal("expected all tiers failed after exhausting list")
	}
	list.ResetTier()
	list.MoveToStoppedAllowedTier()
	if got := list.GetAnnounce(); got != "http://a" {
		t.Fatalf("MoveToStoppedAllowedTier() announce = %q, want http://a", got)
	}
}

func TestAnnounceListMoveToCompletedAllowedTier(t *testing.T) {
	list := NewAnnounceList([][]string{{"http://a"}, {"http://b"}})
	list.AnnounceSuccess()
	list.AnnounceFailure()
	list.ResetTier()
	list.MoveToCompletedAllowedTier()

	if got := list.GetAnnounce(); got != "http://a" {
		t.Fatalf("MoveToCompletedAllowedTier() announce = %q, want http://a", got)
	}
	if !list.CurrentTierAcceptsCompletedEvent() {
		t.Fatal("current tier should accept completed event")
	}
}
