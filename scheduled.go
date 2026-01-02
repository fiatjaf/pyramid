package main

import (
	"context"
	"iter"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

// this relay doesn't deserve its own directory because it's too simple and intertwined with the main relay

var scheduled *khatru.Relay

func initScheduledRelay() {
	scheduled = khatru.NewRelay()
	db := global.IL.Scheduled
	scheduled = khatru.NewRelay()
	scheduled.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/scheduled"

	scheduled.UseEventstore(db, 100)
	scheduled.QueryStored = func(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
		return func(yield func(nostr.Event) bool) {
			for evt := range db.QueryEvents(filter, 100) {
				// people can only see their own scheduled events
				if khatru.IsAuthed(ctx, evt.PubKey) {
					if !yield(evt) {
						return
					}
				}
			}
		}
	}
	scheduled.OnConnect = khatru.RequestAuth
	scheduled.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
		func(ctx context.Context, filter nostr.Filter) (bool, string) {
			// only allow authed users to access scheduled events
			authedPublicKeys := khatru.GetAllAuthed(ctx)
			if len(authedPublicKeys) == 0 {
				return true, "auth-required: can only see your own scheduled notes"
			}

			for _, authed := range authedPublicKeys {
				if pyramid.IsMember(authed) {
					return false, ""
				}
			}

			return true, "restricted: you're not even a relay member"
		},
	)

	scheduled.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)
	scheduled.OnEvent = func(ctx context.Context, event nostr.Event) (reject bool, msg string) {
		return true, "send your notes to the main relay with a future timestamp"
	}
	scheduled.PreventBroadcast = func(ws *khatru.WebSocket, filter nostr.Filter, event nostr.Event) bool {
		for _, pk := range ws.AuthedPublicKeys {
			if pk == event.PubKey {
				return false
			}
		}
		return true
	}

	// start the scheduled events processor
	go processScheduledEvents()
}

func processScheduledEvents() {
	for {
		time.Sleep(time.Second * 65)

		if !global.Settings.AcceptScheduledEvents {
			continue
		}

		// query events that are due to be published
		for event := range global.IL.Scheduled.QueryEvents(nostr.Filter{
			Until: nostr.Now() + 60,
		}, 1000) {
			// move to main relay and broadcast
			if err := global.IL.Main.SaveEvent(event); err != nil {
				log.Error().Err(err).Stringer("event", event).Msg("failed to move scheduled event to main")
				continue
			}

			// delete from scheduled
			if err := global.IL.Scheduled.DeleteEvent(event.ID); err != nil {
				log.Error().Err(err).Stringer("event", event).Msg("failed to delete scheduled event")
				continue
			}

			// broadcast to main relay clients would be handled by main relay
			n := relay.BroadcastEvent(event)
			log.Info().Stringer("event", event).Int("broadcasted", n).Msg("published scheduled event")
		}
	}
}
