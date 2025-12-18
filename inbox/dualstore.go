package inbox

import (
	"iter"
	"slices"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"github.com/fiatjaf/pyramid/global"
)

type dualLayerStore struct{}

func (_ *dualLayerStore) SaveEvent(evt nostr.Event) error {
	if slices.Contains(secretKinds, evt.Kind) {
		return global.IL.Secret.SaveEvent(evt)
	} else {
		return global.IL.Inbox.SaveEvent(evt)
	}
}

func (_ *dualLayerStore) Init() error {
	// layers are already initialized by global.Init()
	return nil
}

func (_ *dualLayerStore) Close() {
	// layers are managed by global.MMMM, so we don't close them here
}

func (_ *dualLayerStore) CountEvents(filter nostr.Filter) (uint32, error) {
	// note: only counts from normalDB, acceptable since CountEvents isn't expected to be used for secret kinds
	return global.IL.Inbox.CountEvents(filter)
}

func (_ *dualLayerStore) DeleteEvent(id nostr.ID) error {
	if err := global.IL.Inbox.DeleteEvent(id); err != nil {
		return err
	}
	if err := global.IL.Secret.DeleteEvent(id); err != nil {
		return err
	}
	return nil
}

func (_ *dualLayerStore) ReplaceEvent(evt nostr.Event) error {
	if slices.Contains(secretKinds, evt.Kind) {
		return global.IL.Secret.ReplaceEvent(evt)
	} else {
		return global.IL.Inbox.ReplaceEvent(evt)
	}
}

func (_ *dualLayerStore) QueryEvents(filter nostr.Filter, limit int) iter.Seq[nostr.Event] {
	if len(filter.Kinds) == 0 {
		// only normal kinds or no kinds specified
		return global.IL.Inbox.QueryEvents(filter, limit)
	}

	secretFilter := filter
	secretFilter.Kinds = nil
	normalFilter := filter
	normalFilter.Kinds = nil
	for _, kind := range filter.Kinds {
		if slices.Contains(secretKinds, kind) {
			secretFilter.Kinds = append(secretFilter.Kinds, kind)
		} else {
			normalFilter.Kinds = append(normalFilter.Kinds, kind)
		}
	}

	if len(secretFilter.Kinds) > 0 && len(normalFilter.Kinds) > 0 {
		// mixed kinds - need to split the filter and query both
		return eventstore.SortedMerge(
			global.IL.Inbox.QueryEvents(normalFilter, 500),
			global.IL.Secret.QueryEvents(secretFilter, 500),
		)
	} else if len(secretFilter.Kinds) > 0 && len(normalFilter.Kinds) == 0 {
		// only secret kinds requested
		return global.IL.Secret.QueryEvents(filter, limit)
	} else {
		// only normal kinds requested
		return global.IL.Inbox.QueryEvents(filter, limit)
	}
}
