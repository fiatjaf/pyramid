package inbox

import (
	"iter"
	"sync"
	"sync/atomic"

	"fiatjaf.com/nostr"
	"github.com/bits-and-blooms/bloom/v3"
	"github.com/puzpuzpuz/xsync/v3"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

type memberBanState struct {
	mu    sync.RWMutex
	bloom *bloom.BloomFilter
	count atomic.Int64
}

type reportRow struct {
	Date        nostr.Timestamp
	ID          nostr.ID
	Target      nostr.PubKey
	Reason      string
	TargetEvent nostr.ID
}

var banState = xsync.NewMapOf[nostr.PubKey, *memberBanState]()

func rejectReport(evt nostr.Event) (bool, string) {
	if !pyramid.IsMember(evt.PubKey) {
		return true, "only relay members can publish reports"
	}

	p := evt.Tags.Find("p")
	if p == nil {
		return true, "missing pubkey target"
	}

	target, err := nostr.PubKeyFromHex(p[1])
	if err != nil {
		return true, "invalid reported pubkey"
	}

	found := false
	for range global.IL.Inbox.QueryEvents(nostr.Filter{Authors: []nostr.PubKey{target}}, 1) {
		found = true
		break
	}
	if !found {
		return true, "we don't know anything about this pubkey"
	}

	return false, ""
}

func rebuildBanState() {
	banState.Clear()
	for report := range global.IL.InboxReports.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{1984}}, global.Settings.Limits.MaxQueryLimit) {
		addReportToBanState(report)
	}
}

func addReportToBanState(report nostr.Event) {
	// add the reported pubkey to the ban state
	p := report.Tags.Find("p")
	if p == nil {
		return
	}
	target, err := nostr.PubKeyFromHex(p[1])
	if err != nil {
		return
	}
	addPubKeyToBanState(report.PubKey, target)
}

func possiblyDeleteReportedEvent(report nostr.Event) {
	// delete this event specifically
	if e := report.Tags.Find("e"); e != nil {
		if id, err := nostr.IDFromHex(e[1]); err == nil {
			for targetEvent := range global.IL.Inbox.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
				for p := range targetEvent.Tags.FindAll("p") {
					if pubkey, err := nostr.PubKeyFromHex(p[1]); err == nil {
						if member := pubkey; pyramid.IsMember(pubkey) && !isBannedByMember(member, targetEvent.PubKey) {
							// this is acceptable to at least one member, so don't delete
							return
						}
					}
				}
			}
		}
	}
}

func addPubKeyToBanState(member, targetPubKey nostr.PubKey) {
	state, loaded := banState.LoadOrStore(member, &memberBanState{bloom: bloom.NewWithEstimates(1000, 0.001)})
	if !loaded {
		state, _ = banState.Load(member)
	}
	state.mu.Lock()
	state.bloom.Add(targetPubKey[:])
	state.mu.Unlock()
	state.count.Add(1)
}

func bannedCount(member nostr.PubKey) int64 {
	state, ok := banState.Load(member)
	if !ok {
		return 0
	}
	return state.count.Load()
}

func isBannedByMember(member, target nostr.PubKey) bool {
	state, ok := banState.Load(member)
	if ok {
		// this member has reports, so test against that
		state.mu.RLock()
		matched := state.bloom.Test(target[:])
		state.mu.RUnlock()

		if matched {
			// looks like this person is banned, double-check:
			for range global.IL.InboxReports.QueryEvents(nostr.Filter{
				Kinds:   []nostr.Kind{1984},
				Authors: []nostr.PubKey{member},
				Tags: nostr.TagMap{
					"p": []string{target.Hex()},
				},
			}, 1) {
				// a report actually exists, so this person is really banned
				return true
			}
		}
	}

	return false
}

func reportsForMember(member nostr.PubKey) iter.Seq[reportRow] {
	return func(yield func(reportRow) bool) {
		for report := range global.IL.InboxReports.QueryEvents(nostr.Filter{
			Kinds:   []nostr.Kind{1984},
			Authors: []nostr.PubKey{member},
		}, global.Settings.Limits.MaxQueryLimit) {
			p := report.Tags.Find("p")
			if p != nil {
				pubkey, err := nostr.PubKeyFromHex(p[1])
				if err != nil {
					continue
				}

				var targetEvent nostr.ID
				var reason string
				if len(p) >= 3 {
					reason = p[2]
				} else if e := report.Tags.Find("e"); e != nil && len(e) >= 3 {
					targetEvent, _ = nostr.IDFromHex(e[1])
					reason = e[2]
				}

				rr := reportRow{
					Date:        report.CreatedAt,
					ID:          report.ID,
					Target:      pubkey,
					Reason:      reason,
					TargetEvent: targetEvent,
				}

				if !yield(rr) {
					return
				}
			}
		}
	}
}
