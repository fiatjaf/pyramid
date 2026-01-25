package search

import (
	"os"
	"path/filepath"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/slicestore"
	"github.com/pemistahl/lingua-go"
	"github.com/stretchr/testify/require"
)

func TestSearch(t *testing.T) {
	detector = lingua.NewLanguageDetectorBuilder().
		FromLanguages(languages...).
		Build()

	tempDir, err := os.MkdirTemp("", "test_search_pyramid")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	db := &slicestore.SliceStore{}
	db.Init()

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
			Content:   "Ahoy mateys! I've discovered a treasure chest filled with gold doubloons and silver pieces buried beneath the old palm tree on Skull Island. The secret map shows an X marks the spot where the legendary pirate Blackbeard hid his most valuable plunder. The chest contains rubies, emeralds, and ancient coins from sunken Spanish galleons.",
			Tags:      nil,
		},
		{
			ID:        nostr.MustIDFromHex("0000000000000000000000000000000000000000000000000000000000000002"),
			PubKey:    nostr.MustPubKeyFromHex("0000000000000000000000000000000000000000000000000000000000000002"),
			CreatedAt: nostr.Timestamp(1609545600),
			Kind:      1111,
			Content:   "The treasure map I found reveals the location of Captain Morgan's lost gold mine deep in the Caribbean waters. Following the ancient compass directions leads to a hidden cave filled with golden artifacts, jeweled swords, and the crown jewels of forgotten kingdoms. The secret passage is guarded by mysterious symbols only known to the brotherhood of the sea.",
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
			Content:   "Bom dia seus piratas melequentos, onde está esse bendito tesouro?",
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
			name: "search for 'treasure'",
			filter: nostr.Filter{
				Search: "treasure",
			},
			expected: 3, // all events mention treasure
		},
		{
			name: "search for 'gold'",
			filter: nostr.Filter{
				Search: "gold",
			},
			expected: 3, // all events mention gold
		},
		{
			name: "search for 'secret map'",
			filter: nostr.Filter{
				Search: "\"secret map\"",
			},
			expected: 1, // only one event mentions secret map
		},
		{
			name: "search for 'emerald'",
			filter: nostr.Filter{
				Search: "+astronomical +emeralds",
			},
			expected: 0, // no events mention emeralds together with astronomical
		},
		{
			name: "search with kind filter",
			filter: nostr.Filter{
				Search: "treasure guards",
				Kinds:  []nostr.Kind{1},
			},
			expected: 2, // only two events are kind 1
		},
		{
			name: "search for 'nonexistent'",
			filter: nostr.Filter{
				Search: "nonexistent",
			},
			expected: 0, // no results
		},
		{
			name: "search with AND",
			filter: nostr.Filter{
				Search: "captain AND gold",
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
