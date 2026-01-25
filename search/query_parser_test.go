package search

import (
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/stretchr/testify/require"
)

func TestParseQuery(t *testing.T) {
	mapping := bleve.NewIndexMapping()
	mapping.DefaultAnalyzer = "en"
	index, err := bleve.NewMemOnly(mapping)
	require.NoError(t, err)

	docs := []map[string]interface{}{
		{"id": "1", "content": "I like fruit especially banana and strawberry"},
		{"id": "2", "content": "I like fruit like apples and oranges"},
		{"id": "3", "content": "I like vegetables but not fruit"},
		{"id": "4", "content": "Banana bread is delicious"},
		{"id": "5", "content": "Strawberry jam and banana smoothie"},
	}

	for _, doc := range docs {
		err := index.Index(doc["id"].(string), doc)
		require.NoError(t, err)
	}

	testQueries := []struct {
		query        string
		expected     int
		exactMatches []string
	}{
		{"fruit", 3, nil},
		{"banana (NOT delicious)", 2, nil},
		{"banana (NOT delicious) bread", 0, nil},
		{"smoothie OR apples", 2, nil},
		{"smoothie OR apples (NOT fruit)", 1, nil},
		{"\"I like\"", 3, []string{"I like"}},
		{"banana \"I like fruit\" strawberries", 1, []string{"I like fruit"}},
		{"\"I like fruit\" (strawberry OR apple)", 2, []string{"I like fruit"}},
	}

	for _, test := range testQueries {
		query, exactMatches, err := parse(test.query)
		require.NoError(t, err)

		require.Equal(t, test.exactMatches, exactMatches)

		search := bleve.NewSearchRequest(query)
		results, err := index.Search(search)
		require.NoError(t, err)

		require.Equal(t, test.expected, int(results.Total),
			"query '%s' expected %d results, got %d", test.query, test.expected, results.Total)
	}
}
