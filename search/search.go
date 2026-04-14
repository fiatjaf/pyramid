package search

import (
	"fmt"
	"path/filepath"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/bleve"
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
	Main *bleve.BleveBackend

	indexableKinds = []nostr.Kind{0, 1, 6, 11, 16, 20, 21, 22, 24, 1111, 9802, 30023, 30818}
)

func Init() error {
	languages := make([]lingua.Language, 0, len(global.Settings.Search.Languages))
	for _, code := range global.Settings.Search.Languages {
		isoCode := lingua.GetIsoCode639_1FromValue(code)
		lang := lingua.GetLanguageFromIsoCode639_1(isoCode)
		languages = append(languages, lang)
	}

	bleveIndex := &bleve.BleveBackend{
		Path:          filepath.Join(global.S.DataPath, "search/main"),
		RawEventStore: global.IL.Main,

		IndexableKinds: indexableKinds,
		Languages:      languages,
	}
	if err := bleveIndex.Init(); err != nil {
		return fmt.Errorf("failed to init search database: %w", err)
	}

	Main = bleveIndex

	return nil
}

func End() {
	Main.Close()
}
