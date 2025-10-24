package uppermost

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

func NewRelay(db *mmm.IndexingLayer) *khatru.Relay {
	relay := khatru.NewRelay()

	relay.ServiceURL = "wss://" + global.Settings.Domain + "/uppermost"
	relay.Info.Name = global.Settings.RelayName + " - uppermost"
	relay.Info.Description = "auto-curated posts with highest quality reactions from relay members."
	relay.Info.Contact = global.Settings.RelayContact
	relay.Info.Icon = global.Settings.RelayIcon

	relay.Info.Software = "https://github.com/fiatjaf/pyramid"

	relay.UseEventstore(db, 500)

	relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
	)

	relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)

	relay.OnEvent = func(ctx context.Context, evt nostr.Event) (bool, string) {
		return true, "restricted: read-only relay"
	}

	relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		uppermostPage(loggedUser).Render(r.Context(), w)
	})

	return relay
}
