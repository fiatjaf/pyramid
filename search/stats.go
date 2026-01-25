package search

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fiatjaf/pyramid/global"
)

const (
	statsFileName = "search_stats.json"
)

var (
	statsMutex sync.RWMutex
	statsFile  string
)

// SearchStats holds timestamps for various search-related operations
type SearchStats struct {
	LastSearchOn        time.Time `json:"last_search_on"`
	LastReindex         time.Time `json:"last_reindex"`
	LastLanguagesChange time.Time `json:"last_languages_change"`
	DocumentCount       int64     `json:"-"`
}

// initializeStatsFile sets up the stats file path
func initializeStatsFile() {
	statsFile = filepath.Join(global.S.DataPath, "search", statsFileName)
}

// loadStats loads the search statistics from the JSON file
func loadStats() (*SearchStats, error) {
	initializeStatsFile()

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

// saveStats saves the search statistics to the JSON file
func saveStats(stats *SearchStats) error {
	initializeStatsFile()

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
	statsMutex.Lock()
	defer statsMutex.Unlock()

	stats, err := loadStats()
	if err != nil {
		return err
	}

	stats.LastSearchOn = time.Now()
	return saveStats(stats)
}

// UpdateReindex updates the timestamp when reindex is called
func UpdateReindex() error {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	stats, err := loadStats()
	if err != nil {
		return err
	}

	stats.LastReindex = time.Now()
	return saveStats(stats)
}

// UpdateLanguagesChange updates the timestamp when languages are changed
func UpdateLanguagesChange() error {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	stats, err := loadStats()
	if err != nil {
		return err
	}

	stats.LastLanguagesChange = time.Now()
	return saveStats(stats)
}

// GetStats returns the current search statistics, including the current document count
func GetStats() (*SearchStats, error) {
	statsMutex.RLock()
	defer statsMutex.RUnlock()

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
