package uppermost

import (
	"context"
	"net/http"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"
	"fiatjaf.com/nostr/nip11"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log   = global.Log.With().Str("relay", "uppermost").Logger()
	Relay *khatru.Relay
)

func Init() {
	if global.Settings.Uppermost.Enabled {
		// relay enabled
		setupEnabled()
	} else {
		// relay disabled
		setupDisabled()
	}
}

func setupDisabled() {
	Relay = khatru.NewRelay()
	Relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		uppermostPage(loggedUser).Render(r.Context(), w)
	})
	Relay.Router().HandleFunc("POST /enable", enableHandler)
}

func setupEnabled() {
	db := global.IL.Uppermost

	Relay = khatru.NewRelay()

	Relay.ServiceURL = "wss://" + global.Settings.Domain + "/uppermost"

	Relay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		info.Name = global.Settings.Uppermost.Name
		if info.Name == "" {
			info.Name = global.Settings.RelayName + " - uppermost"
		}
		info.Description = global.Settings.Uppermost.Description
		if info.Description == "" {
			info.Description = "posts that are highly appreciated by relay members."
		}
		info.Icon = global.Settings.Uppermost.Icon
		if info.Icon == "" {
			info.Icon = global.Settings.RelayIcon
		}
		info.Contact = global.Settings.RelayContact
		info.Software = "https://github.com/fiatjaf/pyramid"
		return info
	}

	Relay.UseEventstore(db, 500)

	Relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
	)

	Relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)

	Relay.OnEvent = func(ctx context.Context, evt nostr.Event) (bool, string) {
		return true, "restricted: read-only relay"
	}

	Relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		uppermostPage(loggedUser).Render(r.Context(), w)
	})
	Relay.Router().HandleFunc("POST /disable", disableHandler)
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Uppermost.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/uppermost/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Uppermost.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/", 302)
}
