package search

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/fiatjaf/pyramid/global"
)

// SearchStats holds timestamps for various search-related operations
type SearchStats struct {
	LastSearchOn        time.Time `json:"last_search_on"`
	LastReindex         time.Time `json:"last_reindex"`
	LastLanguagesChange time.Time `json:"last_languages_change"`
	DocumentCount       int64     `json:"-"`
}

func getStatsFilepath() string { return filepath.Join(global.S.DataPath, "search", "ts.json") }

// loadStats loads the search statistics from the JSON file (must be called with lock held)
func loadStats() (*SearchStats, error) {
	statsFile := getStatsFilepath()

	stats := &SearchStats{}

	// Check if file exists
	if _, err := os.Stat(statsFile); os.IsNotExist(err) {
		// File doesn't exist, return empty stats
		return stats, nil
	}

	// Read and parse existing file
	data, err := os.ReadFile(statsFile)
	if err != nil {
		return nil, err
	}

	if len(data) > 0 {
		if err := json.Unmarshal(data, stats); err != nil {
			return nil, err
		}
	}

	return stats, nil
}

// saveStatsLocked saves the search statistics to the JSON file (must be called with lock held)
func saveStatsLocked(stats *SearchStats) error {
	statsFile := getStatsFilepath()

	// ensure directory exists
	dir := filepath.Dir(statsFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(statsFile, data, 0644)
}

// UpdateSearchOn updates the timestamp when search is turned on
func UpdateSearchOn() error {
	stats, err := loadStats()
	if err != nil {
		return err
	}

	stats.LastSearchOn = time.Now()
	return saveStatsLocked(stats)
}

// UpdateReindex updates the timestamp when reindex is called
func UpdateReindex() error {
	stats, err := loadStats()
	if err != nil {
		return err
	}

	stats.LastReindex = time.Now()
	return saveStatsLocked(stats)
}

// UpdateLanguagesChange updates the timestamp when languages are changed
func UpdateLanguagesChange() error {
	stats, err := loadStats()
	if err != nil {
		return err
	}

	stats.LastLanguagesChange = time.Now()
	return saveStatsLocked(stats)
}

// GetStats returns the current search statistics, including the current document count
func GetStats() (*SearchStats, error) {
	stats, err := loadStats()
	if err != nil {
		return nil, err
	}

	// update document count from index if search is enabled and index is available
	if Main != nil && Main.index != nil {
		if count, err := Main.index.DocCount(); err == nil {
			stats.DocumentCount = int64(count)
		}
	}

	return stats, nil
}

// ShouldShowReindexButton returns true if reindex button should be shown
// (if anything has changed we should show it)
func ShouldShowReindexButton() bool {
	stats, err := GetStats()
	if err != nil {
		log.Warn().Err(err).Msg("GetStats() call failed")
		return false
	}

	if stats.LastReindex.IsZero() {
		return true
	}

	olderThanSearchOn := !stats.LastSearchOn.IsZero() && stats.LastReindex.Before(stats.LastSearchOn)
	olderThanLanguagesChange := !stats.LastLanguagesChange.IsZero() && stats.LastReindex.Before(stats.LastLanguagesChange)

	return olderThanSearchOn || olderThanLanguagesChange
}
