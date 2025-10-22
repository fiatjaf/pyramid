package inbox

import (
	"iter"
	"slices"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
)

type dualLayerStore struct {
	normalDB *mmm.IndexingLayer
	secretDB *mmm.IndexingLayer
}

func (d *dualLayerStore) SaveEvent(evt nostr.Event) error {
	if slices.Contains(secretKinds, evt.Kind) {
		return d.secretDB.SaveEvent(evt)
	} else {
		return d.normalDB.SaveEvent(evt)
	}
}

func (d *dualLayerStore) Init() error {
	// layers are already initialized by global.Init()
	return nil
}

func (d *dualLayerStore) Close() {
	// layers are managed by global.MMMM, so we don't close them here
}

func (d *dualLayerStore) CountEvents(filter nostr.Filter) (uint32, error) {
	// note: only counts from normalDB, acceptable since CountEvents isn't expected to be used for secret kinds
	return d.normalDB.CountEvents(filter)
}

func (d *dualLayerStore) DeleteEvent(id nostr.ID) error {
	if err := d.normalDB.DeleteEvent(id); err != nil {
		return err
	}
	if err := d.secretDB.DeleteEvent(id); err != nil {
		return err
	}
	return nil
}

func (d *dualLayerStore) ReplaceEvent(evt nostr.Event) error {
	if slices.Contains(secretKinds, evt.Kind) {
		return d.secretDB.ReplaceEvent(evt)
	} else {
		return d.normalDB.ReplaceEvent(evt)
	}
}

func (d *dualLayerStore) QueryEvents(filter nostr.Filter, limit int) iter.Seq[nostr.Event] {
	hasSecret := false
	hasNormal := false
	if len(filter.Kinds) > 0 {
		for _, kind := range filter.Kinds {
			if slices.Contains(secretKinds, kind) {
				hasSecret = true
			} else {
				hasNormal = true
			}
		}
	}

	if hasSecret && !hasNormal {
		// only secret kinds requested
		return d.secretDB.QueryEvents(filter, limit)
	} else if hasSecret && hasNormal {
		// mixed kinds - need to split the filter
		return func(yield func(nostr.Event) bool) {
			secretFilter := filter
			secretFilter.Kinds = nil
			for _, kind := range filter.Kinds {
				if slices.Contains(secretKinds, kind) {
					secretFilter.Kinds = append(secretFilter.Kinds, kind)
				}
			}

			normalFilter := filter
			normalFilter.Kinds = nil
			for _, kind := range filter.Kinds {
				if !slices.Contains(secretKinds, kind) {
					normalFilter.Kinds = append(normalFilter.Kinds, kind)
				}
			}

			// query both
			if len(secretFilter.Kinds) > 0 {
				for evt := range d.secretDB.QueryEvents(secretFilter, limit) {
					if !yield(evt) {
						return
					}
				}
			}
			if len(normalFilter.Kinds) > 0 {
				for evt := range d.normalDB.QueryEvents(normalFilter, limit) {
					if !yield(evt) {
						return
					}
				}
			}
		}
	} else {
		// only normal kinds or no kinds specified
		return d.normalDB.QueryEvents(filter, limit)
	}
}
