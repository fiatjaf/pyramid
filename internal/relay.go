package internal

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
	log   = global.Log.With().Str("relay", "internal").Logger()
	Relay *khatru.Relay
)

func Init() {
	if global.Settings.Internal.Enabled {
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
		internalPage(loggedUser).Render(r.Context(), w)
	})
	Relay.Router().HandleFunc("POST /enable", enableHandler)
}

func setupEnabled() {
	db := global.IL.Internal

	Relay = khatru.NewRelay()

	Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Internal.HTTPBasePath

	Relay.ManagementAPI.ChangeRelayName = changeInternalRelayNameHandler
	Relay.ManagementAPI.ChangeRelayDescription = changeInternalRelayDescriptionHandler
	Relay.ManagementAPI.ChangeRelayIcon = changeInternalRelayIconHandler

	Relay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		info.Name = global.Settings.Internal.GetName()
		info.Description = global.Settings.Internal.GetDescription()
		info.Icon = global.Settings.Internal.GetIcon()
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
		policies.MustAuth,
		func(ctx context.Context, _ nostr.Filter) (bool, string) {
			authedPublicKeys := khatru.GetAllAuthed(ctx)
			if len(authedPublicKeys) == 0 {
				return true, "auth-required: this is only viewable by relay members"
			}

			for _, authed := range authedPublicKeys {
				if pyramid.IsMember(authed) {
					return false, ""
				}
			}

			return true, "restricted: you're not a relay member"
		},
	)

	Relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)

	Relay.OnEvent = policies.SeqEvent(
		policies.PreventLargeContent(10000),
		policies.PreventTooManyIndexableTags(9, []nostr.Kind{3}, nil),
		policies.PreventTooManyIndexableTags(1200, nil, []nostr.Kind{3}),
		policies.RestrictToSpecifiedKinds(true, 1, 11, 1111, 1444, 1244, 20, 21, 22, 31924, 31925, 31922, 31923, 30818),
		policies.OnlyAllowNIP70ProtectedEvents,
		func(ctx context.Context, evt nostr.Event) (bool, string) {
			if pyramid.IsMember(evt.PubKey) {
				return false, ""
			}
			return true, "restricted: must be a relay member"
		},
	)

	Relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		internalPage(loggedUser).Render(r.Context(), w)
	})
	Relay.Router().HandleFunc("POST /disable", disableHandler)
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Internal.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/"+global.Settings.Internal.HTTPBasePath+"/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Internal.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/"+global.Settings.Internal.HTTPBasePath+"/", 302)
}

func changeInternalRelayNameHandler(ctx context.Context, name string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Internal.Name = name
	return global.SaveUserSettings()
}

func changeInternalRelayDescriptionHandler(ctx context.Context, description string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Internal.Description = description
	return global.SaveUserSettings()
}

func changeInternalRelayIconHandler(ctx context.Context, icon string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Internal.Icon = icon
	return global.SaveUserSettings()
}
