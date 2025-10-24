package inbox

import (
	"context"
	"net/http"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"

	"github.com/fiatjaf/pyramid/global"
)

var (
	allowedKinds  = []nostr.Kind{9802, 1, 1111, 11, 1244, 1222, 30818, 20, 21, 22, 30023}
	secretKinds   = []nostr.Kind{1059}
	aggregatedWoT WotXorFilter

	log = global.Log.With().Str("relay", "inbox").Logger()
)

func NewRelay(normalDB *mmm.IndexingLayer, secretDB *mmm.IndexingLayer) *khatru.Relay {
	relay := khatru.NewRelay()

	relay.ServiceURL = "wss://" + global.Settings.Domain + "/inbox"
	relay.Info.Name = global.Settings.RelayName + " - inbox"
	relay.Info.Description = "filtered notifications for relay members using unified web of trust."
	relay.Info.Contact = global.Settings.RelayContact
	relay.Info.Icon = global.Settings.RelayIcon

	relay.Info.Software = "https://github.com/fiatjaf/pyramid"

	// use dual layer store
	dualStore := &dualLayerStore{
		normalDB: normalDB,
		secretDB: secretDB,
	}
	relay.UseEventstore(dualStore, 500)

	relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
		rejectFilter,
	)

	relay.OnEvent = rejectEvent

	relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)

	relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		inboxPage(loggedUser).Render(r.Context(), w)
	})

	// compute aggregated WoT in background
	go func() {
		ctx := context.Background()
		wot, err := computeAggregatedWoT(ctx)
		if err != nil {
			nostr.InfoLogger.Println("failed to compute aggregated WoT:", err)
			return
		}
		aggregatedWoT = wot
		nostr.InfoLogger.Printf("computed aggregated WoT with %d entries", wot.Items)
	}()

	return relay
}
