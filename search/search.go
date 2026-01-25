package search

import (
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/nip27"
	"fiatjaf.com/nostr/sdk"
	bleve "github.com/blevesearch/bleve/v2"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ar"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/da"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/de"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/en"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/es"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/fa"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/fi"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/fr"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/gl"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/hi"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/hr"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/hu"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/in"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/it"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/nl"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/no"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/pl"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/pt"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ro"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ru"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/sv"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/tr"
	bleveMapping "github.com/blevesearch/bleve/v2/mapping"
	bleveQuery "github.com/blevesearch/bleve/v2/search/query"
	"github.com/fiatjaf/pyramid/global"
	"github.com/pemistahl/lingua-go"
)

const (
	labelContentField    = "c"
	labelKindField       = "k"
	labelCreatedAtField  = "a"
	labelPubkeyField     = "p"
	labelReferencesField = "r"
)

var (
	log  = global.Log.With().Str("relay", "grasp").Logger()
	Main *BleveIndex

	indexableKinds = []nostr.Kind{0, 1, 11, 24, 1111, 30023, 30818}
	languages      = []lingua.Language{lingua.GetLanguageFromIsoCode639_1(lingua.AR), lingua.GetLanguageFromIsoCode639_1(lingua.DA), lingua.GetLanguageFromIsoCode639_1(lingua.DE), lingua.GetLanguageFromIsoCode639_1(lingua.EN), lingua.GetLanguageFromIsoCode639_1(lingua.ES), lingua.GetLanguageFromIsoCode639_1(lingua.FA), lingua.GetLanguageFromIsoCode639_1(lingua.FI), lingua.GetLanguageFromIsoCode639_1(lingua.FR), lingua.GetLanguageFromIsoCode639_1(lingua.HI), lingua.GetLanguageFromIsoCode639_1(lingua.HR), lingua.GetLanguageFromIsoCode639_1(lingua.HU), lingua.GetLanguageFromIsoCode639_1(lingua.IT), lingua.GetLanguageFromIsoCode639_1(lingua.NL), lingua.GetLanguageFromIsoCode639_1(lingua.PL), lingua.GetLanguageFromIsoCode639_1(lingua.PT), lingua.GetLanguageFromIsoCode639_1(lingua.RO), lingua.GetLanguageFromIsoCode639_1(lingua.RU), lingua.GetLanguageFromIsoCode639_1(lingua.SV), lingua.GetLanguageFromIsoCode639_1(lingua.TR)}
	detector       lingua.LanguageDetector
)

type BleveIndex struct {
	sync.Mutex
	// Path is where the index will be saved
	Path string

	// RawEventStore is where we'll fetch the raw events from
	// bleve will only store ids, so the actual events must be somewhere else
	RawEventStore eventstore.Store

	index bleve.Index
}

func Init() error {
	Main = &BleveIndex{
		Path:          filepath.Join(global.S.DataPath, "search/main"),
		RawEventStore: global.IL.Main,
	}
	if err := Main.Init(); err != nil {
		return fmt.Errorf("failed to init search database: %w", err)
	}

	BuildLanguageDetector()

	return nil
}

func End() {
	Main.Close()
}

func Reindex() {
	for event := range global.IL.Main.QueryEvents(nostr.Filter{Kinds: indexableKinds}, 10_000_000) {
		if err := Main.SaveEvent(event); err != nil {
			log.Warn().Err(err).Stringer("event", event).Msg("failed to index event")
		} else {
			log.Debug().Str("event", event.ID.Hex()).Msg("indexed event")
		}
	}
}

func BuildLanguageDetector() {
	languages := make([]lingua.Language, 0, 2)

	for _, code := range global.Settings.Search.Languages {
		isoCode := lingua.GetIsoCode639_1FromValue(code)
		lang := lingua.GetLanguageFromIsoCode639_1(isoCode)
		languages = append(languages, lang)
	}
	if len(languages) == 0 {
		languages = append(languages, lingua.English)
	}

	detector = lingua.NewLanguageDetectorBuilder().
		FromLanguages(languages...).
		Build()
}

func (b *BleveIndex) IndexEvent(event nostr.Event) error {
	if b == Main {
		if slices.Contains(indexableKinds, event.Kind) {
			return b.SaveEvent(event)
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

		for _, lang := range languages {
			code := strings.ToLower(lang.IsoCode639_1().String())

			contentField := bleveMapping.NewTextFieldMapping()
			contentField.Store = false
			contentField.IncludeTermVectors = false
			contentField.DocValues = false
			contentField.Analyzer = code
			contentField.IncludeInAll = true
			doc.AddFieldMappingsAt(labelContentField+"_"+code, contentField)
		}

		referencesField := bleveMapping.NewKeywordFieldMapping()
		referencesField.IncludeInAll = true
		referencesField.DocValues = false
		referencesField.Store = false
		referencesField.IncludeTermVectors = false
		doc.AddFieldMappingsAt(labelReferencesField, referencesField)

		authorField := bleveMapping.NewKeywordFieldMapping()
		authorField.Store = false
		authorField.IncludeTermVectors = false
		authorField.DocValues = false
		doc.AddFieldMappingsAt(labelPubkeyField, authorField)

		kindField := bleveMapping.NewKeywordFieldMapping()
		kindField.Store = false
		kindField.IncludeTermVectors = false
		kindField.DocValues = false
		kindField.IncludeInAll = false
		doc.AddFieldMappingsAt(labelKindField, kindField)

		timestampField := bleveMapping.NewDateTimeFieldMapping()
		timestampField.Store = false
		timestampField.IncludeTermVectors = false
		timestampField.DocValues = false
		timestampField.IncludeInAll = false
		doc.AddFieldMappingsAt(labelCreatedAtField, timestampField)

		mapping.AddDocumentMapping("_default", doc)

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
	doc := map[string]any{
		labelKindField:      strconv.Itoa(int(evt.Kind)),
		labelPubkeyField:    evt.PubKey.Hex()[56:],
		labelCreatedAtField: evt.CreatedAt.Time(),
	}

	var content string
	var references []string

	if evt.Kind == 0 {
		var pm sdk.ProfileMetadata
		if err := json.Unmarshal([]byte(evt.Content), &pm); err == nil {
			evt.Content = pm.Name + " " + pm.DisplayName + " " + pm.About
			references = append(references, pm.NIP05)
		}
	}

	for block := range nip27.Parse(evt.Content) {
		if block.Pointer == nil {
			content += block.Text
		} else {
			references = append(references, block.Pointer.AsTagReference())
		}
	}

	lang, ok := detector.DetectLanguageOf(content)
	if !ok {
		lang = lingua.English
	}
	doc[labelContentField+"_"+strings.ToLower(lang.IsoCode639_1().String())] = content

	// exact matching:
	doc[labelReferencesField] = references

	if err := b.index.Index(evt.ID.Hex(), doc); err != nil {
		return fmt.Errorf("failed to index '%s' document: %w", evt.ID, err)
	}

	return nil
}

func (b *BleveIndex) DeleteEvent(id nostr.ID) error {
	return b.index.Delete(id.Hex())
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

		// use query parser for complex search syntax
		languages := []string{"en"}
		if len(global.Settings.Search.Languages) > 0 {
			languages = global.Settings.Search.Languages
		}
		contentQueries := make([]bleveQuery.Query, 0, len(languages))

		// search all the language fields we have configured
		searchQ, exactMatches, err := parse(filter.Search, labelContentField+"_"+languages[0])
		if err != nil {
			// fallback to simple match query on parse error
			log.Warn().Err(err).Str("search", filter.Search).Msg("parse error, falling back to simple match")
			for _, code := range languages {
				match := bleve.NewMatchQuery(filter.Search)
				match.SetField(labelContentField + "_" + code)
				contentQueries = append(contentQueries, match)
			}
		} else {
			contentQueries = append(contentQueries, searchQ)
			for _, code := range languages[1:] {
				searchQ, _, _ := parse(filter.Search, labelContentField+"_"+code)
				contentQueries = append(contentQueries, searchQ)
			}
		}
		var q bleveQuery.Query = bleveQuery.NewDisjunctionQuery(contentQueries)

		// gather other fields from the filter
		conjQueries := []bleveQuery.Query{}
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
			var min time.Time
			if filter.Since != 0 {
				min = filter.Since.Time()
			}
			var max time.Time
			if filter.Until != 0 {
				max = filter.Until.Time()
			} else {
				max = time.Now()
			}
			dateRangeQ := bleve.NewDateRangeQuery(min, max)
			dateRangeQ.SetField(labelCreatedAtField)
			conjQueries = append(conjQueries, dateRangeQ)
		}

		if len(conjQueries) > 0 {
			conjQueries = append(conjQueries, q)
			q = bleveQuery.NewConjunctionQuery(conjQueries)
		}

		req := bleve.NewSearchRequest(q)
		req.Size = maxLimit
		req.From = 0
		req.Explain = true

		result, err := b.index.Search(req)
		if err != nil {
			return
		}

	resultHit:
		for _, hit := range result.Hits {
			id, err := nostr.IDFromHex(hit.ID)
			if err != nil {
				continue
			}
			for evt := range b.RawEventStore.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
				for _, exactMatch := range exactMatches {
					if !strings.Contains(evt.Content, exactMatch) {
						continue resultHit
					}
				}

				for f, v := range filter.Tags {
					if !evt.Tags.ContainsAny(f, v) {
						continue resultHit
					}
				}

				if !yield(evt) {
					return
				}
			}
		}
	}
}
