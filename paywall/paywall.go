package paywall

import (
	"context"
	"iter"
	"slices"
	"sync"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/puzpuzpuz/xsync/v3"
)

type meta struct {
	source  string
	id      nostr.ID
	when    nostr.Timestamp
	comment string
}

var userPaywallMap = xsync.NewMapOf[nostr.PubKey, map[nostr.PubKey]meta]()

// PaywallReference associates an addressable or replaceable event from a kind:1163 event from Author with a
// a filter such that when such replaceable event is updated we know to react.
type PaywallReference struct {
	// the pyramid member who referenced this for their paywall
	Member nostr.PubKey

	// taken from the "a" tag in the kind:1163 from Member, could be anything
	PublicKey  nostr.PubKey
	Kind       nostr.Kind
	Identifier string
}

var References []PaywallReference

func IsReferenced(event nostr.Event) bool {
	for _, ref := range References {
		if event.PubKey == ref.PublicKey && event.Kind == ref.Kind && event.Tags.GetD() == ref.Identifier {
			return true
		}
	}
	return false
}

func ReferencedBy(event nostr.Event) iter.Seq[nostr.PubKey] {
	return func(yield func(nostr.PubKey) bool) {
		for _, ref := range References {
			if event.PubKey == ref.PublicKey && event.Kind == ref.Kind && event.Tags.GetD() == ref.Identifier {
				yield(ref.Member)
			}
		}
	}
}

// CanRead checks if a reader can access an author's paywalled content
func CanRead(author, reader nostr.PubKey) bool {
	if author == reader {
		return true
	}
	if userMap, ok := userPaywallMap.Load(author); ok {
		_, exists := userMap[reader]
		return exists
	}
	return false
}

var mu sync.Mutex

// RecomputeMemberPaywall rebuilds paywall access map for a given user
// by reading all their kind:1163 events and collecting "p" tags
// It also reads "a" tags to build filters for referenced events
func RecomputeMemberPaywall(ctx context.Context, member nostr.PubKey) {
	mu.Lock()
	defer mu.Unlock()

	// build new map of who can read this user's content
	newReaders := make(map[nostr.PubKey]meta)

	var referencesToKeep []int
	var newReferences []PaywallReference

	for evt := range global.IL.Main.QueryEvents(nostr.Filter{
		Authors: []nostr.PubKey{member},
		Kinds:   []nostr.Kind{1163},
	}, 1000) {
		// collect all "p" tags (pubkeys) from this event
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "p" {
				if pk, err := nostr.PubKeyFromHex(tag[1]); err == nil {
					newReaders[pk] = meta{source: "direct", id: evt.ID, when: evt.CreatedAt, comment: evt.Content}
				}
			}
		}

		// collect all "a" tags and build filters
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "a" {
				if ptr, err := nostr.ParseAddrString(tag[1]); err == nil {
					// check if this reference already exists for this author
					ref := PaywallReference{
						Member:     member,
						Identifier: ptr.Identifier,
						PublicKey:  ptr.PublicKey,
						Kind:       ptr.Kind,
					}
					if idx := slices.Index(References, ref); idx == -1 {
						// doesn't exist, so this is new
						newReferences = append(newReferences, ref)
					} else {
						// exists, so we keep
						referencesToKeep = append(referencesToKeep, idx)
					}

					// add members of this list to the readers list (if the list exists in this relay)
					for refEvt := range global.IL.Main.QueryEvents(ptr.AsFilter(), 1) {
						for _, tag := range refEvt.Tags {
							if len(tag) >= 2 && tag[0] == "p" {
								if pk, err := nostr.PubKeyFromHex(tag[1]); err == nil {
									newReaders[pk] = meta{source: "reference", id: refEvt.ID, when: evt.CreatedAt, comment: evt.Content}
								}
							}
						}
					}
				}
			}
		}
	}

	// now actually update the global references list
	for i := 0; i < len(References); i++ {
		r := References[i]
		if r.Member != member {
			continue
		}
		if slices.Contains(referencesToKeep, i) {
			continue
		}

		// we must delete this (so let's try to replace it first)
		if len(newReferences) > 0 {
			References[i] = newReferences[len(newReferences)-1]
			newReferences = newReferences[:len(newReferences)-1]
		} else {
			// otherwise swap-delete
			References[i] = References[len(References)-1]
			References = References[:len(References)-1]
			i-- // and repeat this loop position
		}
	}
	// add any remaining references now
	for _, ref := range newReferences {
		References = append(References, ref)
	}

	// update readers map
	userPaywallMap.Store(member, newReaders)
}
