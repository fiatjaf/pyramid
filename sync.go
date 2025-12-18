package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/wrappers"
	"fiatjaf.com/nostr/nip77"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/layout"
)

func streamingSync(
	ctx context.Context,
	loggedUser nostr.PubKey,
	remoteUrl string,
	upload,
	download bool,
	w http.ResponseWriter,
) {
	// set up streaming response
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	writer := bufio.NewWriter(w)

	// send initial HTML
	initialHTML := bytes.NewBuffer(nil)
	layout.Layout(loggedUser, "sync").Render(ctx, initialHTML)
	spl := bytes.Split(initialHTML.Bytes(), []byte("<header"))
	writer.Write(spl[0])
	writer.Write([]byte("<table class='w-full border border-separate'>"))
	writer.Flush()
	flusher.Flush()

	// create a channel to capture progress updates
	progress := make(chan string, 100)
	done := make(chan error, 1)

	// start sync in goroutine
	go func() {
		// send initial message
		progress <- "starting sync"

		// create wrappers to adapt IndexingLayer to nostr interfaces
		local := wrappers.StorePublisher{
			Store:    global.IL.Main,
			MaxLimit: 1_000_000, // large limit for comprehensive sync
		}
		var source nostr.Querier = local
		var target nostr.Publisher = local

		if !download {
			target = nil
		}
		if !upload {
			source = nil
		}

		// use the wrappers as source and target for two-way sync
		err := nip77.NegentropySync(
			ctx,
			remoteUrl,
			nostr.Filter{
				Authors: []nostr.PubKey{loggedUser},
			},
			source,
			target,
			func(ctx context.Context, dir nip77.Direction) {
				source := "local"
				target := remoteUrl
				if dir.From != local {
					source = remoteUrl
					target = "local"
				}

				select {
				case progress <- "syncing events from " + source + " to " + target:
				case <-ctx.Done():
					return
				}

				// this is only necessary because relays are too ratelimiting
				batch := make([]nostr.ID, 0, 50)

				seen := make(map[nostr.ID]struct{})
				for item := range dir.Items {
					if _, ok := seen[item]; ok {
						continue
					}
					progress <- fmt.Sprintf("event %s found on %s", item.Hex(), source)
					seen[item] = struct{}{}

					batch = append(batch, item)
					if len(batch) == 50 {
						for evt := range dir.From.QueryEvents(nostr.Filter{IDs: batch}) {
							progress <- fmt.Sprintf("publishing %s to %s", evt, target)
							dir.To.Publish(ctx, evt)
						}
						batch = batch[:0]
					}
				}

				if len(batch) > 0 {
					for evt := range dir.From.QueryEvents(nostr.Filter{IDs: batch}) {
						progress <- fmt.Sprintf("publishing %s to %s", evt, target)
						dir.To.Publish(ctx, evt)
					}
				}
			})

		select {
		case done <- err:
		case <-ctx.Done():
		}
	}()

	// handle progress updates and completion
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case msg := <-progress:
			fmt.Fprintf(writer, `<tr><td class="px-3 border">%s</td></tr>`, msg)
			writer.Flush()
			flusher.Flush()

		case err := <-done:
			if err != nil {
				fmt.Fprintf(writer, `<tr><td class="px-3 border text-red-500">sync failed: %s</td></tr>`, err.Error())
			} else {
				fmt.Fprint(writer, `<tr><td class="px-3 border text-emerald-500">sync complete</td></tr>`)
			}
			writer.Flush()
			flusher.Flush()

			// close HTML
			fmt.Fprint(writer, `
    </table>
</body>
</html>`)
			writer.Flush()
			flusher.Flush()
			return

		case <-ticker.C:
			// periodic flush to keep connection alive
			writer.Flush()
			flusher.Flush()

		case <-ctx.Done():
			fmt.Fprint(writer, `<tr><td class="px-3 border text-amber-500">sync interrupted</td></tr>`)
			writer.Flush()
			flusher.Flush()
			return
		}
	}
}
