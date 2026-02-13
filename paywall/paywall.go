package paywall

import (
	"context"
	"time"

	"fiatjaf.com/nostr"
	"github.com/bep/debounce"
	"github.com/fiatjaf/pyramid/global"
	"github.com/puzpuzpuz/xsync/v3"
)

var (
	userPaywallMap = xsync.NewMapOf[nostr.PubKey, map[nostr.PubKey]bool]()
	debouncer      = debounce.New(time.Second * 30)
)

// CanRead checks if a reader can access an author's paywalled content
func CanRead(author, reader nostr.PubKey) bool {
	if author == reader {
		return true
	}
	if userMap, ok := userPaywallMap.Load(author); ok {
		return userMap[reader]
	}
	return false
}

// RecomputeUserPaywall rebuilds paywall access map for a given user
// by reading all their kind:1163 events and collecting "p" tags
func RecomputeUserPaywall(ctx context.Context, author nostr.PubKey) {
	debouncer(func() {
		// build new map of who can read this user's content
		newReaders := make(map[nostr.PubKey]bool)
		for evt := range global.IL.Main.QueryEvents(nostr.Filter{
			Authors: []nostr.PubKey{author},
			Kinds:   []nostr.Kind{1163},
		}, 100) {
			// collect all "p" tags (pubkeys) from this event
			for _, tag := range evt.Tags {
				if len(tag) >= 2 && tag[0] == "p" {
					if pk, err := nostr.PubKeyFromHex(tag[1]); err == nil {
						newReaders[pk] = true
					}
				}
			}
		}

		// update global map
		userPaywallMap.Store(author, newReaders)
	})
}
