package wot

import (
	"context"
	"sync/atomic"
	"time"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
)

// current holds the latest computed aggregated web-of-trust filter.
// it is stored behind an atomic pointer so it can be read from request
// goroutines while the background goroutine replaces it, without racing
// on the multi-word XorFilter struct.
var current atomic.Pointer[XorFilter]

// computed is set to true after the first successful WoT computation.
var computed atomic.Bool

// Contains reports whether the given pubkey is in the latest aggregated WoT.
// It is safe to call at any time, even before the first computation
// (it will just return false).
func Contains(pubkey nostr.PubKey) bool {
	f := current.Load()
	if f == nil {
		return false
	}
	return f.Contains(pubkey)
}

// IsComputed reports whether the WoT has been computed at least once.
func IsComputed() bool {
	return computed.Load()
}

// Count returns the number of pubkeys in the latest aggregated WoT.
func Count() int {
	f := current.Load()
	if f == nil {
		return 0
	}
	return f.Items
}

// StartBackgroundComputation begins a periodic background loop that
// recomputes the aggregated WoT every 48 hours. It sleeps 2 minutes
// before the first computation to let the system stabilise.
func StartBackgroundComputation() {
	go func() {
		ctx := context.Background()
		time.Sleep(time.Minute * 2)
		for {
			wot, err := ComputeAggregated(ctx)
			if err != nil {
				global.Log.Error().Err(err).Msg("failed to compute aggregated WoT")
				time.Sleep(3 * time.Hour)
			} else {
				current.Store(&wot)
				computed.Store(true)
				global.Log.Info().Int("entries", wot.Items).Msg("computed aggregated WoT")
				time.Sleep(48 * time.Hour)
			}
		}
	}()
}
