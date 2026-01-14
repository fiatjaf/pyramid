package global

import (
	"context"
	"iter"
	"slices"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
)

// QueryStoredWithPinned is a custom QueryStored function that returns pinned events first when that makes sense
func QueryStoredWithPinned(relayId string) func(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	return func(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
		var store *mmm.IndexingLayer
		var get func() *nostr.Event

		switch relayId {
		case "internal":
			store = IL.Internal
			get = func() *nostr.Event { return PinnedCache.Internal }
		case "favorites":
			store = IL.Favorites
			get = func() *nostr.Event { return PinnedCache.Favorites }
		case "popular":
			store = IL.Popular
			get = func() *nostr.Event { return PinnedCache.Popular }
		case "uppermost":
			store = IL.Uppermost
			get = func() *nostr.Event { return PinnedCache.Uppermost }
		case "moderated":
			store = IL.Moderated
			get = func() *nostr.Event { return PinnedCache.Moderated }
		}

		return func(yield func(nostr.Event) bool) {
			pinned := get()

			if pinned != nil &&
				filter.IDs == nil && filter.Tags == nil && filter.Authors == nil &&
				filter.Until == 0 && filter.Since < pinned.CreatedAt &&
				(filter.Kinds == nil || slices.Contains(filter.Kinds, pinned.Kind)) {
				// display pinned in this case
				if !yield(*pinned) {
					return
				}

				if filter.Limit > 0 {
					// we've used one limit
					filter.Limit--
				}
			}

			// then return normal query results
			for event := range store.QueryEvents(filter, 500) {
				if !yield(event) {
					return
				}
			}
		}
	}
}

func CachePinnedEvent(relayId string) {
	var store *mmm.IndexingLayer
	var pinnedID nostr.ID
	var set func(*nostr.Event)

	switch relayId {
	case "main":
		store = IL.Main
		set = func(evt *nostr.Event) {
			PinnedCache.Main = evt
		}
		pinnedID = Settings.Pinned
	case "internal":
		store = IL.Internal
		set = func(evt *nostr.Event) {
			PinnedCache.Internal = evt
		}
		pinnedID = Settings.Internal.Pinned
	case "favorites":
		store = IL.Favorites
		set = func(evt *nostr.Event) {
			PinnedCache.Favorites = evt
		}
		pinnedID = Settings.Favorites.Pinned
	case "popular":
		store = IL.Popular
		set = func(evt *nostr.Event) {
			PinnedCache.Popular = evt
		}
		pinnedID = Settings.Popular.Pinned
	case "uppermost":
		store = IL.Uppermost
		set = func(evt *nostr.Event) {
			PinnedCache.Uppermost = evt
		}
		pinnedID = Settings.Uppermost.Pinned
	case "moderated":
		store = IL.Moderated
		set = func(evt *nostr.Event) {
			PinnedCache.Moderated = evt
		}
		pinnedID = Settings.Moderated.Pinned
	}

	if pinnedID == nostr.ZeroID {
		set(nil)
	} else {
		for evt := range store.QueryEvents(nostr.Filter{IDs: []nostr.ID{pinnedID}}, 1) {
			set(&evt)
			break
		}
	}

}

var PinnedCache struct {
	Main      *nostr.Event
	Internal  *nostr.Event
	Favorites *nostr.Event
	Popular   *nostr.Event
	Uppermost *nostr.Event
	Moderated *nostr.Event
}
