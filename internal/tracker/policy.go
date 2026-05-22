package tracker

import (
	"strings"
	"time"
)

const defaultAnnounceInterval = 2 * time.Minute

type AnnounceEvent int

const (
	AnnounceStarted AnnounceEvent = iota
	AnnounceStartedAfterCompletion
	AnnounceDownloading
	AnnounceStopped
	AnnounceCompleted
	AnnounceSeeding
	AnnounceHalted
)

type announceTier struct {
	event AnnounceEvent
	urls  []string
}

type AnnounceList struct {
	tiers   []announceTier
	tierIdx int
	urlIdx  int
	current bool
}

func NewAnnounceList(tiers [][]string) *AnnounceList {
	list := &AnnounceList{}
	for _, tier := range tiers {
		if len(tier) == 0 {
			continue
		}
		urls := make([]string, len(tier))
		copy(urls, tier)
		list.tiers = append(list.tiers, announceTier{
			event: AnnounceStarted,
			urls:  urls,
		})
	}
	list.resetIterator()
	return list
}

func SplitTrackerSpecs(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func NormalizeAnnounceTiers(announce string, announceList [][]string, excludeSpecs, addSpecs []string) [][]string {
	var tiers [][]string
	if announceList != nil {
		for _, tier := range announceList {
			if len(tier) == 0 {
				continue
			}
			urls := make([]string, len(tier))
			copy(urls, tier)
			tiers = append(tiers, urls)
		}
	} else if strings.TrimSpace(announce) != "" {
		tiers = append(tiers, []string{strings.TrimSpace(announce)})
	}

	exclude := SplitTrackerSpecs(excludeSpecs)
	add := SplitTrackerSpecs(addSpecs)

	if len(exclude) > 0 {
		if containsString(exclude, "*") {
			tiers = nil
		} else {
			filtered := tiers[:0]
			for _, tier := range tiers {
				next := tier[:0]
				for _, uri := range tier {
					if !containsString(exclude, uri) {
						next = append(next, uri)
					}
				}
				if len(next) > 0 {
					urls := make([]string, len(next))
					copy(urls, next)
					filtered = append(filtered, urls)
				}
			}
			tiers = filtered
		}
	}

	for _, uri := range add {
		tiers = append(tiers, []string{uri})
	}

	return tiers
}

func (l *AnnounceList) CountTiers() int {
	return len(l.tiers)
}

func (l *AnnounceList) AllTiersFailed() bool {
	return !l.current
}

func (l *AnnounceList) ResetTier() {
	l.resetIterator()
}

func (l *AnnounceList) GetAnnounce() string {
	if !l.current {
		return ""
	}
	return l.tiers[l.tierIdx].urls[l.urlIdx]
}

func (l *AnnounceList) GetEvent() AnnounceEvent {
	if !l.current {
		return AnnounceStarted
	}
	return l.tiers[l.tierIdx].event
}

func (l *AnnounceList) SetEvent(event AnnounceEvent) {
	if !l.current {
		return
	}
	l.tiers[l.tierIdx].event = event
}

func (l *AnnounceList) GetEventString() string {
	if !l.current {
		return ""
	}
	switch l.tiers[l.tierIdx].event {
	case AnnounceStarted, AnnounceStartedAfterCompletion:
		return "started"
	case AnnounceStopped:
		return "stopped"
	case AnnounceCompleted:
		return "completed"
	default:
		return ""
	}
}

func (l *AnnounceList) AnnounceSuccess() {
	if !l.current {
		return
	}
	tier := &l.tiers[l.tierIdx]
	tier.event = nextEvent(tier.event)
	if l.urlIdx > 0 && l.urlIdx < len(tier.urls) {
		url := tier.urls[l.urlIdx]
		copy(tier.urls[1:l.urlIdx+1], tier.urls[0:l.urlIdx])
		tier.urls[0] = url
	}
	l.resetIterator()
}

func (l *AnnounceList) AnnounceFailure() {
	if !l.current {
		return
	}
	l.urlIdx++
	if l.urlIdx < len(l.tiers[l.tierIdx].urls) {
		return
	}
	l.tiers[l.tierIdx].event = nextEventIfAfterStarted(l.tiers[l.tierIdx].event)
	l.tierIdx++
	l.urlIdx = 0
	l.current = l.tierIdx < len(l.tiers)
}

func (l *AnnounceList) CountStoppedAllowedTier() int {
	n := 0
	for _, tier := range l.tiers {
		if tierAcceptsStoppedEvent(tier.event) {
			n++
		}
	}
	return n
}

func (l *AnnounceList) CountCompletedAllowedTier() int {
	n := 0
	for _, tier := range l.tiers {
		if tierAcceptsCompletedEvent(tier.event) {
			n++
		}
	}
	return n
}

func (l *AnnounceList) CurrentTierAcceptsStoppedEvent() bool {
	return l.current && tierAcceptsStoppedEvent(l.tiers[l.tierIdx].event)
}

func (l *AnnounceList) CurrentTierAcceptsCompletedEvent() bool {
	return l.current && tierAcceptsCompletedEvent(l.tiers[l.tierIdx].event)
}

func (l *AnnounceList) MoveToStoppedAllowedTier() {
	l.moveToMatchingTier(tierAcceptsStoppedEvent)
}

func (l *AnnounceList) MoveToCompletedAllowedTier() {
	l.moveToMatchingTier(tierAcceptsCompletedEvent)
}

func (l *AnnounceList) resetIterator() {
	if len(l.tiers) == 0 {
		l.tierIdx = 0
		l.urlIdx = 0
		l.current = false
		return
	}
	l.tierIdx = 0
	l.urlIdx = 0
	l.current = len(l.tiers[0].urls) > 0
}

func (l *AnnounceList) moveToMatchingTier(match func(AnnounceEvent) bool) {
	if len(l.tiers) == 0 {
		l.current = false
		return
	}
	start := l.tierIdx
	if start < 0 || start >= len(l.tiers) {
		start = 0
	}
	for step := 0; step < len(l.tiers); step++ {
		idx := (start + step) % len(l.tiers)
		if match(l.tiers[idx].event) {
			l.tierIdx = idx
			l.urlIdx = 0
			l.current = len(l.tiers[idx].urls) > 0
			return
		}
	}
	l.current = false
}

func nextEvent(event AnnounceEvent) AnnounceEvent {
	switch event {
	case AnnounceStarted:
		return AnnounceDownloading
	case AnnounceStartedAfterCompletion, AnnounceCompleted:
		return AnnounceSeeding
	case AnnounceStopped:
		return AnnounceHalted
	default:
		return event
	}
}

func nextEventIfAfterStarted(event AnnounceEvent) AnnounceEvent {
	switch event {
	case AnnounceStopped:
		return AnnounceHalted
	case AnnounceCompleted:
		return AnnounceSeeding
	default:
		return event
	}
}

func tierAcceptsStoppedEvent(event AnnounceEvent) bool {
	switch event {
	case AnnounceDownloading, AnnounceStopped, AnnounceCompleted, AnnounceSeeding:
		return true
	default:
		return false
	}
}

func tierAcceptsCompletedEvent(event AnnounceEvent) bool {
	switch event {
	case AnnounceDownloading, AnnounceCompleted:
		return true
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
