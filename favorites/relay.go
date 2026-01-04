package favorites

import (
	"context"
	"fmt"
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
	log   = global.Log.With().Str("relay", "favorites").Logger()
	Relay *khatru.Relay
)

func Init() {
	if global.Settings.Favorites.Enabled {
		// relay enabled
		setupEnabled()
	} else {
		// relay disabled
		setupDisabled()
	}
}

func setupDisabled() {
	Relay = khatru.NewRelay()
	Relay.Router().HandleFunc("/"+global.Settings.Favorites.HTTPBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		favoritesPage(loggedUser).Render(r.Context(), w)
	})
	Relay.Router().HandleFunc("POST /"+global.Settings.Favorites.HTTPBasePath+"/enable", enableHandler)
}

func setupEnabled() {
	db := global.IL.Favorites

	Relay = khatru.NewRelay()

	Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Favorites.HTTPBasePath

	Relay.ManagementAPI.ChangeRelayName = changeFavoritesRelayNameHandler
	Relay.ManagementAPI.ChangeRelayDescription = changeFavoritesRelayDescriptionHandler
	Relay.ManagementAPI.ChangeRelayIcon = changeFavoritesRelayIconHandler

	Relay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		info.Name = global.Settings.Favorites.GetName()
		info.Description = global.Settings.Favorites.GetDescription()
		info.Icon = global.Settings.Favorites.GetIcon()
		info.Contact = global.Settings.RelayContact
		info.Software = "https://github.com/fiatjaf/pyramid"
		return info
	}
	Relay.UseEventstore(db, 500)

	pk := global.Settings.RelayInternalSecretKey.Public()
	Relay.Info.Self = &pk
	Relay.Info.PubKey = &pk

	Relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
	)

	Relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)

	Relay.OnEvent = policies.SeqEvent(
		policies.PreventLargeContent(10000),
		policies.PreventTooManyIndexableTags(9, []nostr.Kind{3}, nil),
		policies.PreventTooManyIndexableTags(1200, nil, []nostr.Kind{3}),
		policies.RestrictToSpecifiedKinds(true, 1, 11, 1111, 1222, 1244, 30023, 30818, 9802, 20, 21, 22),
		func(ctx context.Context, evt nostr.Event) (bool, string) {
			authedPublicKeys := khatru.GetAllAuthed(ctx)
			if len(authedPublicKeys) == 0 {
				return true, "auth-required: must be a relay member"
			}

			for _, authed := range authedPublicKeys {
				if evt.PubKey == authed {
					return true, "blocked: can't save your own event here"
				}

				if pyramid.IsMember(authed) {
					// got our authenticated user, so this ok
					return false, ""
				}
			}

			return true, "restricted: you're not a relay member"
		},
	)

	Relay.Router().HandleFunc("/"+global.Settings.Favorites.HTTPBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		favoritesPage(loggedUser).Render(r.Context(), w)
	})
	Relay.Router().HandleFunc("POST /"+global.Settings.Favorites.HTTPBasePath+"/disable", disableHandler)
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Favorites.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/"+global.Settings.Favorites.HTTPBasePath+"/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Favorites.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/"+global.Settings.Favorites.HTTPBasePath+"/", 302)
}

func changeFavoritesRelayNameHandler(ctx context.Context, name string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Favorites.Name = name
	return global.SaveUserSettings()
}

func changeFavoritesRelayDescriptionHandler(ctx context.Context, description string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Favorites.Description = description
	return global.SaveUserSettings()
}

func changeFavoritesRelayIconHandler(ctx context.Context, icon string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Favorites.Icon = icon
	return global.SaveUserSettings()
}
