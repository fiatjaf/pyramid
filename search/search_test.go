package search

import (
	"os"
	"path/filepath"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/slicestore"
	"github.com/fiatjaf/pyramid/global"
	"github.com/pemistahl/lingua-go"
	"github.com/stretchr/testify/require"
)

func TestSearch(t *testing.T) {
	detector = lingua.NewLanguageDetectorBuilder().
		FromLanguages(Languages...).
		Build()

	tempDir, err := os.MkdirTemp("", "test_search_pyramid")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	db := &slicestore.SliceStore{}
	db.Init()

	global.Settings.Search.Languages = []string{
		"en",
		"pt",
	}
	BuildLanguageDetector()

	index := &BleveIndex{
		Path:          filepath.Join(tempDir, "test_index"),
		RawEventStore: db,
	}
	err = index.Init()
	require.NoError(t, err)
	defer index.Close()

	pirateEvents := []nostr.Event{
		{
			ID:        nostr.MustIDFromHex("0000000000000000000000000000000000000000000000000000000000000001"),
			PubKey:    nostr.MustPubKeyFromHex("0000000000000000000000000000000000000000000000000000000000000001"),
			CreatedAt: nostr.Timestamp(1609459200),
			Kind:      1,
			Content:   "Ahoy mateys! I've discovered a treasure chest filled with gold doubloons and silver pieces buried beneath the old palm tree on Skull Island. The secret map shows an X marks the spot where the legendary pirate Blackbeard hid his most valuable plunder. The chest contains rubies, emeralds, and ancient coins from sunken Spanish galleons. https://www.youtube.com/watch?v=enTAromEeHo&t=88s",
			Tags:      nil,
		},
		{
			ID:        nostr.MustIDFromHex("0000000000000000000000000000000000000000000000000000000000000002"),
			PubKey:    nostr.MustPubKeyFromHex("0000000000000000000000000000000000000000000000000000000000000001"),
			CreatedAt: nostr.Timestamp(1609545600),
			Kind:      1111,
			Content:   "The treasure map I found reveals the location of Captain Morgan's lost gold mine deep in the Caribbean waters. Following the ancient compass directions leads to a hidden cave filled with golden artifacts, jeweled swords, and the crown jewels of forgotten kingdoms. The secret passage is guarded by mysterious symbols only known to the brotherhood of the sea. https://www.youtube.com/watch?v=yBtyNIqZios",
			Tags:      nil,
		},
		{
			ID:        nostr.MustIDFromHex("0000000000000000000000000000000000000000000000000000000000000003"),
			PubKey:    nostr.MustPubKeyFromHex("0000000000000000000000000000000000000000000000000000000000000003"),
			CreatedAt: nostr.Timestamp(1609632000),
			Kind:      1,
			Content:   "Legends speak of the Emerald City of the Lost Pirates, a mythical place where streets are paved with gold and buildings adorned with precious gems. The secret entrance can only be found during a full moon when the tides reveal a hidden path across the coral reefs. Ancient scrolls tell of guardians protecting treasure vaults containing the world's most valuable gems.",
			Tags:      nil,
		},
		{
			ID:        nostr.MustIDFromHex("0000000000000000000000000000000000000000000000000000000000000004"),
			PubKey:    nostr.MustPubKeyFromHex("0000000000000000000000000000000000000000000000000000000000000004"),
			CreatedAt: nostr.Timestamp(1609545601),
			Kind:      1111,
			Content:   "Bom dia seus piratas melequentos, onde está esse bendito tesouro? nostr:nprofile1qqsv6jemsnaq925ddfqjhwm3du3k0zk7dnj2ksk2k4hcfkf80mzf56spz9mhxue69uhkzcnpvdshgefwvdhk6tmjzyj",
			Tags:      nil,
		},
		{
			ID:        nostr.MustIDFromHex("0000000000000000000000000000000000000000000000000000000000000005"),
			PubKey:    nostr.MustPubKeyFromHex("0000000000000000000000000000000000000000000000000000000000000005"),
			CreatedAt: nostr.Timestamp(1609545602),
			Kind:      30023,
			Content:   "I pirati dei Caraibi del XVII e XVIII secolo sono diventati leggendari per la loro ricerca di tesori. Questi avventurieri del mare saccheggiavano navi cariche d'oro, argento e pietre preziose provenienti dalle colonie spagnole del Nuovo Mondo.\n\nSecondo la leggenda, molti pirati seppellivano i loro tesori su isole remote, creando mappe segrete con la famosa \"X\" che segnava il punto. Capitani famosi come Barbanera, Capitan Kidd e Henry Morgan sono entrati nell'immaginario collettivo come custodi di ricchezze nascoste.\n\nAnche se la maggior parte dei tesori dei pirati sono probabilmente solo miti, alcuni sono stati davvero ritrovati. Il fascino di questi bottini nascosti continua ad ispirare storie, film e cacciatori di tesori ancora oggi.",
			Tags:      nil,
		},
	}

	for _, event := range pirateEvents {
		err := db.SaveEvent(event)
		require.NoError(t, err)
		err = index.SaveEvent(event)
		require.NoError(t, err)
	}

	testCases := []struct {
		name     string
		filter   nostr.Filter
		expected int
	}{
		{
			name: "search for 'gold'",
			filter: nostr.Filter{
				Search: "gold",
			},
			expected: 3, // all events mention gold
		},
		{
			name: "search for 'treasure'",
			filter: nostr.Filter{
				Search: "treasure",
			},
			expected: 3, // all events mention treasure
		},
		{
			name: "search for 'emerald' together with 'astronomical'",
			filter: nostr.Filter{
				Search: "astronomical emeralds",
			},
			expected: 0, // no events mention emeralds together with astronomical
		},
		{
			name: "search for 'secret map'",
			filter: nostr.Filter{
				Search: "\"secret map\"",
			},
			expected: 1, // only one event mentions secret map
		},
		{
			name: "search with kind filter",
			filter: nostr.Filter{
				Search: "gold",
				Kinds:  []nostr.Kind{1},
			},
			expected: 2, // only two events are kind 1
		},
		{
			name: "search in portuguese",
			filter: nostr.Filter{
				Search: "melequento",
			},
			expected: 1,
		},
		{
			name: "search with exact match",
			filter: nostr.Filter{
				Search: "\"the secret entrance can only be found during a full moon\"",
			},
			expected: 1,
		},
		{
			name: "search with OR across languages",
			filter: nostr.Filter{
				Search: "melequento OR matey",
			},
			expected: 2,
		},
		{
			name: "search with exact reference found in the text",
			filter: nostr.Filter{
				Search: "tesouro nostr:nprofile1qqsv6jemsnaq925ddfqjhwm3du3k0zk7dnj2ksk2k4hcfkf80mzf56spzpmhxue69uhkyctwv9hxztnrdaksmfp5mw", // this is the same pubkey from above, but it's a different nprofile
			},
			expected: 1,
		},
		{
			name: "search for URL",
			filter: nostr.Filter{
				Search: "https://www.youtube.com/watch?v=yBtyNIqZios treasure",
			},
			expected: 1,
		},
		{
			name: "search for host/domain of URL",
			filter: nostr.Filter{
				Search: "www.youtube.com",
			},
			expected: 2,
		},
		{
			name: "mentioning the author should include their notes in the result",
			filter: nostr.Filter{
				Search: " nostr:npub1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqshp52w2",
			},
			expected: 2,
		},
		{
			name: "mentioning the author should include their notes in the result",
			filter: nostr.Filter{
				Search: "found gold? nostr:npub1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqshp52w2",
			},
			expected: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var count int
			for range index.QueryEvents(tc.filter, 10) {
				count++
			}
			require.Equal(t, tc.expected, count)
		})
	}
}
