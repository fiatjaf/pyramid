package global

import (
	"context"
	"iter"
	"slices"
	"strconv"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
)

// QueryStoredWithPinned is a custom QueryStored function that returns pinned events first when that makes sense
func QueryStoredWithPinned(relayId RelayID) func(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	return func(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
		var store *mmm.IndexingLayer
		var get func() *nostr.Event

		switch relayId {
		// we don't handle RelayMain here because that is handled in queryMain at core.go
		//
		case RelayInternal:
			store = IL.Internal
			get = func() *nostr.Event { return PinnedCache.Internal }
		case RelayFavorites:
			store = IL.Favorites
			get = func() *nostr.Event { return PinnedCache.Favorites }
		case RelayPopular:
			store = IL.Popular
			get = func() *nostr.Event { return PinnedCache.Popular }
		case RelayUppermost:
			store = IL.Uppermost
			get = func() *nostr.Event { return PinnedCache.Uppermost }
		case RelayModerated:
			store = IL.Moderated
			get = func() *nostr.Event { return PinnedCache.Moderated }
		}

		return func(yield func(nostr.Event) bool) {
			pinned := get()

			if pinned != nil &&
				filter.IDs == nil && filter.Tags == nil && filter.Authors == nil &&
				filter.Until == 0 && filter.Since < pinned.CreatedAt {

				if y, ok := PreparedPinned(pinned, filter); ok {
					if !yield(y) {
						return
					}
					if filter.Limit > 0 {
						// we've used one limit
						filter.Limit--
					}
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

// if the query allows, we'll wrap the pinned event in a kind:6 repost with "-" tag
// otherwise we will return it naked.
// or not return it at all
func PreparedPinned(pinned *nostr.Event, filter nostr.Filter) (nostr.Event, bool) {
	if pinned.Kind == 1 && (filter.Kinds == nil || slices.Contains(filter.Kinds, 6)) {
		repost := nostr.Event{
			Kind:      6,
			CreatedAt: nostr.Now() + 600,
			Tags: nostr.Tags{
				{"-", ""},
				{"e", pinned.ID.Hex()},
			},
			Content: pinned.String(),
			PubKey:  Settings.RelayInternalSecretKey.Public(),
		}
		repost.Sign(Settings.RelayInternalSecretKey)
		return repost, true
	} else if slices.Contains(filter.Kinds, 16) {
		repost := nostr.Event{
			Kind:      16,
			CreatedAt: nostr.Now() + 600,
			Tags: nostr.Tags{
				{"-", ""},
				{"e", pinned.ID.Hex()},
				{"k", strconv.Itoa(int(pinned.Kind))},
			},
			Content: pinned.String(),
			PubKey:  Settings.RelayInternalSecretKey.Public(),
		}
		repost.Sign(Settings.RelayInternalSecretKey)
		return repost, true
	} else if slices.Contains(filter.Kinds, pinned.Kind) {
		return *pinned, true
	} else {
		return nostr.Event{}, false
	}
}

func CachePinnedEvent(relayId RelayID) {
	var store *mmm.IndexingLayer
	var pinnedID nostr.ID
	var set func(*nostr.Event)

	switch relayId {
	case RelayMain:
		store = IL.Main
		set = func(evt *nostr.Event) {
			PinnedCache.Main = evt
		}
		pinnedID = Settings.Pinned
	case RelayInternal:
		store = IL.Internal
		set = func(evt *nostr.Event) {
			PinnedCache.Internal = evt
		}
		pinnedID = Settings.Internal.Pinned
	case RelayFavorites:
		store = IL.Favorites
		set = func(evt *nostr.Event) {
			PinnedCache.Favorites = evt
		}
		pinnedID = Settings.Favorites.Pinned
	case RelayPopular:
		store = IL.Popular
		set = func(evt *nostr.Event) {
			PinnedCache.Popular = evt
		}
		pinnedID = Settings.Popular.Pinned
	case RelayUppermost:
		store = IL.Uppermost
		set = func(evt *nostr.Event) {
			PinnedCache.Uppermost = evt
		}
		pinnedID = Settings.Uppermost.Pinned
	case RelayModerated:
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
