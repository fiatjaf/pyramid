package global

import (
	"errors"
	"fmt"
	"iter"
	"path/filepath"
	"strconv"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	bleve "github.com/blevesearch/bleve/v2"
	bleveMapping "github.com/blevesearch/bleve/v2/mapping"
	bleveQuery "github.com/blevesearch/bleve/v2/search/query"
)

const (
	labelContentField   = "c"
	labelKindField      = "k"
	labelCreatedAtField = "a"
	labelPubkeyField    = "p"
)

var Search struct {
	Main *BleveIndex
}

var _ eventstore.Store = (*BleveIndex)(nil)

type BleveIndex struct {
	sync.Mutex
	// Path is where the index will be saved
	Path string

	// RawEventStore is where we'll fetch the raw events from
	// bleve will only store ids, so the actual events must be somewhere else
	RawEventStore eventstore.Store

	index bleve.Index
}

func InitSearch() error {
	Search.Main = &BleveIndex{
		Path:          filepath.Join(S.DataPath, "search/main"),
		RawEventStore: IL.Main,
	}
	if err := Search.Main.Init(); err != nil {
		return fmt.Errorf("failed to init search database: %w", err)
	}

	return nil
}

func (b *BleveIndex) IndexEvent(event nostr.Event) error {
	if b == Search.Main {
		switch event.Kind {
		case 1, 11, 24, 1111, 30023, 30818:
			if len(event.Content) > 45 {
				return b.SaveEvent(event)
			}
		}
	}

	return nil
}

func (b *BleveIndex) Close() {
	if b != nil && b.index != nil {
		b.index.Close()
	}
}

func (b *BleveIndex) Init() error {
	if b.Path == "" {
		return fmt.Errorf("missing Path")
	}
	if b.RawEventStore == nil {
		return fmt.Errorf("missing RawEventStore")
	}

	// try to open existing index
	index, err := bleve.Open(b.Path)
	if err == bleve.ErrorIndexPathDoesNotExist {
		// create new index with default mapping
		mapping := bleveMapping.NewIndexMapping()
		mapping.DefaultMapping.Dynamic = false
		doc := bleveMapping.NewDocumentStaticMapping()

		contentField := bleveMapping.NewTextFieldMapping()
		contentField.Store = false
		doc.AddFieldMappingsAt(labelContentField, contentField)

		authorField := bleveMapping.NewKeywordFieldMapping()
		authorField.Store = false
		doc.AddFieldMappingsAt(labelPubkeyField, authorField)

		kindField := bleveMapping.NewNumericFieldMapping()
		kindField.Store = false
		doc.AddFieldMappingsAt(labelKindField, kindField)

		timestampField := bleveMapping.NewDateTimeFieldMapping()
		timestampField.Store = false
		doc.AddFieldMappingsAt(labelCreatedAtField, timestampField)

		mapping.AddDocumentMapping("event", doc)

		index, err = bleve.New(b.Path, mapping)
		if err != nil {
			return fmt.Errorf("error creating index: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("error opening index: %w", err)
	}

	b.index = index
	return nil
}

func (b *BleveIndex) CountEvents(filter nostr.Filter) (uint32, error) {
	if filter.String() == "{}" {
		count, err := b.index.DocCount()
		return uint32(count), err
	}

	return 0, errors.New("not supported")
}

func (b *BleveIndex) SaveEvent(evt nostr.Event) error {
	if err := b.index.Index(evt.ID.Hex(), map[string]any{
		labelContentField:   evt.Content,
		labelKindField:      evt.Kind,
		labelPubkeyField:    evt.PubKey.Hex()[56:],
		labelCreatedAtField: evt.CreatedAt.Time(),
	}); err != nil {
		return fmt.Errorf("failed to index '%s' document: %w", evt.ID, err)
	}

	return nil
}

func (b *BleveIndex) DeleteEvent(id nostr.ID) error {
	return b.index.Delete(id.Hex())
}

func (b *BleveIndex) ReplaceEvent(evt nostr.Event) error {
	b.Lock()
	defer b.Unlock()

	filter := nostr.Filter{Kinds: []nostr.Kind{evt.Kind}, Authors: []nostr.PubKey{evt.PubKey}}
	if evt.Kind.IsAddressable() {
		filter.Tags = nostr.TagMap{"d": []string{evt.Tags.GetD()}}
	}

	shouldStore := true
	for previous := range b.QueryEvents(filter, 1) {
		if nostr.IsOlder(previous, evt) {
			if err := b.DeleteEvent(previous.ID); err != nil {
				return fmt.Errorf("failed to delete event for replacing: %w", err)
			}
		} else {
			shouldStore = false
		}
	}

	if shouldStore {
		if err := b.SaveEvent(evt); err != nil && err != eventstore.ErrDupEvent {
			return fmt.Errorf("failed to save: %w", err)
		}
	}

	return nil
}

func (b *BleveIndex) QueryEvents(filter nostr.Filter, maxLimit int) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		if tlimit := filter.GetTheoreticalLimit(); tlimit == 0 {
			return
		} else if tlimit < maxLimit {
			maxLimit = tlimit
		}

		if len(filter.Search) < 2 {
			return
		}

		searchQ := bleve.NewMatchQuery(filter.Search)
		searchQ.SetField(labelContentField)
		var q bleveQuery.Query = searchQ

		conjQueries := []bleveQuery.Query{searchQ}

		if len(filter.Kinds) > 0 {
			eitherKind := bleve.NewDisjunctionQuery()
			for _, kind := range filter.Kinds {
				kindQ := bleve.NewTermQuery(strconv.Itoa(int(kind)))
				kindQ.SetField(labelKindField)
				eitherKind.AddQuery(kindQ)
			}
			conjQueries = append(conjQueries, eitherKind)
		}

		if len(filter.Authors) > 0 {
			eitherPubkey := bleve.NewDisjunctionQuery()
			for _, pubkey := range filter.Authors {
				if len(pubkey) != 64 {
					continue
				}
				pubkeyQ := bleve.NewTermQuery(pubkey.Hex()[56:])
				pubkeyQ.SetField(labelPubkeyField)
				eitherPubkey.AddQuery(pubkeyQ)
			}
			conjQueries = append(conjQueries, eitherPubkey)
		}

		if filter.Since != 0 || filter.Until != 0 {
			var min *float64
			if filter.Since != 0 {
				minVal := float64(filter.Since)
				min = &minVal
			}
			var max *float64
			if filter.Until != 0 {
				maxVal := float64(filter.Until)
				max = &maxVal
			}
			dateRangeQ := bleve.NewNumericRangeInclusiveQuery(min, max, nil, nil)
			dateRangeQ.SetField(labelCreatedAtField)
			conjQueries = append(conjQueries, dateRangeQ)
		}

		if len(conjQueries) > 1 {
			q = bleve.NewConjunctionQuery(conjQueries...)
		}

		req := bleve.NewSearchRequest(q)
		req.Size = maxLimit
		req.From = 0

		result, err := b.index.Search(req)
		if err != nil {
			return
		}

		for _, hit := range result.Hits {
			id, err := nostr.IDFromHex(hit.ID)
			if err != nil {
				continue
			}
			for evt := range b.RawEventStore.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
				if !yield(evt) {
					return
				}
			}
		}
	}
}
