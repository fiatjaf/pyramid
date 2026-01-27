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
	"fiatjaf.com/nostr/nip73"
	"fiatjaf.com/nostr/sdk"
	bleve "github.com/blevesearch/bleve/v2"
	_ "github.com/blevesearch/bleve/v2/analysis/analyzer/simple"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ar"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/cjk"
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
	"github.com/pemistahl/lingua-go"

	"github.com/fiatjaf/pyramid/global"
)

const (
	// prefix for each of the language-specific content fields
	// (one will be used only for each event)
	labelContentField = "c"

	labelKindField       = "k"
	labelCreatedAtField  = "a"
	labelAuthorField     = "p" // last 8 chars of pubkey of author
	labelReferencesField = "r" // keywords for exact match
	labelExtrasField     = "x" // other things, language-independent
)

var (
	log  = global.Log.With().Str("service", "search").Logger()
	Main *BleveIndex

	indexableKinds = []nostr.Kind{0, 1, 6, 11, 16, 20, 21, 22, 24, 1111, 30023, 30818}
	Languages      = []lingua.Language{
		// each of these translates to a specific bleve analyzer
		// except for japanese-korean-chinese that all use the same "cjk" analyzer
		lingua.Arabic,
		lingua.Chinese,
		lingua.Croatian,
		lingua.Danish,
		lingua.Dutch,
		lingua.English,
		lingua.Finnish,
		lingua.French,
		lingua.German,
		lingua.Hindi,
		lingua.Hungarian,
		lingua.Italian,
		lingua.Japanese,
		lingua.Korean,
		lingua.Persian,
		lingua.Polish,
		lingua.Portuguese,
		lingua.Romanian,
		lingua.Russian,
		lingua.Spanish,
		lingua.Swedish,
		lingua.Turkish,
	}
	detector lingua.LanguageDetector
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

func BuildLanguageDetector() {
	if len(global.Settings.Search.Languages) <= 1 {
		detector = nil
		return
	}

	languages := make([]lingua.Language, 0, len(global.Settings.Search.Languages))

	for _, code := range global.Settings.Search.Languages {
		isoCode := lingua.GetIsoCode639_1FromValue(code)
		lang := lingua.GetLanguageFromIsoCode639_1(isoCode)
		languages = append(languages, lang)
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

		for _, lang := range Languages {
			var analyzerLangCode string
			switch lang {
			case lingua.Japanese:
				analyzerLangCode = "cjk"
			case lingua.Chinese, lingua.Korean:
				// skip, these will also use the cjk analyzer, so we don't have to declare again
				continue
			default:
				// for all other languages the analyzer is the lang 2-letter code
				analyzerLangCode = strings.ToLower(lang.IsoCode639_1().String())
			}

			contentField := bleveMapping.NewTextFieldMapping()
			contentField.Analyzer = analyzerLangCode
			contentField.Store = false
			contentField.IncludeTermVectors = false
			contentField.DocValues = false
			contentField.IncludeInAll = false
			doc.AddFieldMappingsAt(labelContentField+"_"+analyzerLangCode, contentField)
		}

		extrasField := bleveMapping.NewTextFieldMapping()
		extrasField.Analyzer = "simple"
		extrasField.Store = false
		extrasField.IncludeTermVectors = false
		extrasField.DocValues = false
		extrasField.IncludeInAll = false
		doc.AddFieldMappingsAt(labelExtrasField, extrasField)

		referencesField := bleveMapping.NewKeywordFieldMapping()
		referencesField.DocValues = false
		referencesField.Store = false
		referencesField.IncludeTermVectors = false
		referencesField.IncludeInAll = false
		doc.AddFieldMappingsAt(labelReferencesField, referencesField)

		authorField := bleveMapping.NewKeywordFieldMapping()
		authorField.DocValues = false
		authorField.Store = false
		authorField.IncludeTermVectors = false
		authorField.DocValues = false
		doc.AddFieldMappingsAt(labelAuthorField, authorField)

		kindField := bleveMapping.NewKeywordFieldMapping()
		kindField.DocValues = false
		kindField.Store = false
		kindField.IncludeTermVectors = false
		kindField.IncludeInAll = false
		doc.AddFieldMappingsAt(labelKindField, kindField)

		timestampField := bleveMapping.NewDateTimeFieldMapping()
		timestampField.DocValues = false
		timestampField.Store = false
		timestampField.IncludeTermVectors = false
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
	// for kinds 6 and 16 (reposts), try to parse the inner event from Content
	// if valid, use the inner event's fields for indexing but keep the outer event's ID
	docID := evt.ID

	if evt.Kind == 6 || evt.Kind == 16 {
		var innerEvt nostr.Event
		if err := json.Unmarshal([]byte(evt.Content), &innerEvt); err != nil || !innerEvt.VerifySignature() {
			return nil // skip if Content doesn't contain a valid event
		}
		evt = innerEvt
	}

	doc := map[string]any{
		labelKindField:      strconv.Itoa(int(evt.Kind)),
		labelAuthorField:    evt.PubKey.Hex()[56:],
		labelCreatedAtField: evt.CreatedAt.Time(),
	}

	content := strings.Builder{}
	content.Grow(len(evt.Content))

	var references []string
	var extras string

	if evt.Kind == 0 {
		var pm sdk.ProfileMetadata
		if err := json.Unmarshal([]byte(evt.Content), &pm); err == nil {
			evt.Content = pm.Name + " " + pm.DisplayName + " " + pm.About
			references = append(references, pm.NIP05)
		}
	}

	for block := range nip27.Parse(evt.Content) {
		if block.Pointer == nil {
			content.WriteString(strings.TrimSpace(block.Text))
		} else {
			references = append(references, block.Pointer.AsTagReference())
			if ep, ok := block.Pointer.(nip73.ExternalPointer); ok {
				extras += ep.Thing + " "
			}
		}
	}

	var lang lingua.Language
	indexableContent := content.String()
	if len(global.Settings.Search.Languages) == 1 {
		// if only one language is configured, always use that language
		isoCode := lingua.GetIsoCode639_1FromValue(global.Settings.Search.Languages[0])
		lang = lingua.GetLanguageFromIsoCode639_1(isoCode)
	} else {
		// otherwise detect the language
		var ok bool
		lang, ok = detector.DetectLanguageOf(indexableContent)
		if !ok {
			isoCode := lingua.GetIsoCode639_1FromValue(global.Settings.Search.Languages[0])
			lang = lingua.GetLanguageFromIsoCode639_1(isoCode)
		}
	}

	var analyzerLangCode string
	switch lang {
	case lingua.Japanese, lingua.Chinese, lingua.Korean:
		analyzerLangCode = "cjk"
	default:
		analyzerLangCode = strings.ToLower(lang.IsoCode639_1().String())
	}
	doc[labelContentField+"_"+analyzerLangCode] = indexableContent

	// exact matching:
	doc[labelReferencesField] = references

	// other stuff, non-exact matching
	doc[labelExtrasField] = extras

	if err := b.index.Index(docID.Hex(), doc); err != nil {
		return fmt.Errorf("failed to index '%s' document: %w", docID.Hex(), err)
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

		filter.Search = strings.TrimSpace(filter.Search)
		if len(filter.Search) < 2 {
			return
		}

		// search all the language fields we have configured and search for the keywords
		languages := []string{"en"}
		if len(global.Settings.Search.Languages) > 0 {
			languages = global.Settings.Search.Languages
		}
		and := make([]bleveQuery.Query, 0, 3)

		searchC := strings.Builder{}
		searchC.Grow(len(filter.Search))

		for block := range nip27.Parse(filter.Search) {
			if block.Pointer != nil {
				genericRef := bleve.NewTermQuery(block.Pointer.AsTagReference())
				genericRef.SetField(labelReferencesField)
				genericRef.SetBoost(2)

				// if there is a pubkey reference then we should also look for it in the author field
				var ref bleveQuery.Query = genericRef
				if profile, ok := block.Pointer.(nostr.ProfilePointer); ok {
					authorQuery := bleve.NewTermQuery(profile.PublicKey.Hex()[56:])
					authorQuery.SetField(labelAuthorField)
					authorQuery.SetBoost(2)
					orRef := bleve.NewDisjunctionQuery()
					orRef.AddQuery(genericRef)
					orRef.AddQuery(authorQuery)
					ref = orRef
				} else if addr, ok := block.Pointer.(nostr.EntityPointer); ok {
					authorQuery := bleve.NewTermQuery(addr.PublicKey.Hex()[56:])
					authorQuery.SetField(labelAuthorField)
					authorQuery.SetBoost(2)
					orRef := bleve.NewDisjunctionQuery()
					orRef.AddQuery(genericRef)
					orRef.AddQuery(authorQuery)
					ref = orRef
				}
				and = append(and, ref)
			} else {
				searchC.WriteString(strings.TrimSpace(block.Text))
			}
		}

		searchContent := searchC.String()

		// use query parser for complex search syntax
		var exactMatches []string
		if len(searchContent) > 0 {
			contentQueries := make([]bleveQuery.Query, 0, len(languages)+1)

			searchQ, exactMatches_, err := parse(searchContent, labelContentField+"_"+languages[0])
			if err != nil {
				log.Warn().Err(err).Str("search", searchContent).Msg("parse error, falling back to simple match")
				for _, code := range languages {
					match := bleve.NewMatchQuery(searchContent)
					match.SetField(labelContentField + "_" + code)
					contentQueries = append(contentQueries, match)
				}
			} else {
				contentQueries = append(contentQueries, searchQ)
				for _, code := range languages[1:] {
					searchQ, _, _ := parse(searchContent, labelContentField+"_"+code)
					contentQueries = append(contentQueries, searchQ)
				}
			}
			exactMatches = exactMatches_
			// treat extras as if it was another language
			extras := bleve.NewMatchQuery(searchContent)
			extras.SetField(labelExtrasField)
			contentQueries = append(contentQueries, extras)

			// all the content queries go together as an OR and are added to the search tree as a single AND element
			and = append(and, bleveQuery.NewDisjunctionQuery(contentQueries))
		}

		// gather other fields from the filter
		if len(filter.Kinds) > 0 {
			eitherKind := bleve.NewDisjunctionQuery()
			for _, kind := range filter.Kinds {
				kindQ := bleve.NewTermQuery(strconv.Itoa(int(kind)))
				kindQ.SetField(labelKindField)
				eitherKind.AddQuery(kindQ)
			}
			and = append(and, eitherKind)
		}

		if len(filter.Authors) > 0 {
			eitherPubkey := bleve.NewDisjunctionQuery()
			for _, pubkey := range filter.Authors {
				if len(pubkey) != 64 {
					continue
				}
				pubkeyQ := bleve.NewTermQuery(pubkey.Hex()[56:])
				pubkeyQ.SetField(labelAuthorField)
				eitherPubkey.AddQuery(pubkeyQ)
			}
			and = append(and, eitherPubkey)
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
			and = append(and, dateRangeQ)
		}

		q := bleveQuery.NewConjunctionQuery(and)
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
					if !strings.Contains(strings.ToLower(evt.Content), exactMatch) {
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
