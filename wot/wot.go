package wot

import (
	"context"
	"time"

	"fiatjaf.com/nostr"
)

// Current is the latest computed aggregated web-of-trust filter.
// It is safe to read at any time, even before it has been computed
// (it will just return an empty filter that contains nothing).
var Current XorFilter

// Computed is true after the first successful WoT computation.
var Computed bool

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
				nostr.InfoLogger.Println("failed to compute aggregated WoT:", err)
				time.Sleep(3 * time.Hour)
			} else {
				Current = wot
				Computed = true
				nostr.InfoLogger.Printf("computed aggregated WoT with %d entries", wot.Items)
				time.Sleep(48 * time.Hour)
			}
		}
	}()
}
