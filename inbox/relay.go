package inbox

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"
	"fiatjaf.com/nostr/nip11"
	"fiatjaf.com/nostr/nip86"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log   = global.Log.With().Str("relay", "inbox").Logger()
	Relay *khatru.Relay
)

func Init() {
	if global.Settings.Inbox.Enabled {
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
		inboxPage(loggedUser).Render(r.Context(), w)
	})
	Relay.Router().HandleFunc("POST /enable", enableHandler)
}

func setupEnabled() {
	normalDB := global.IL.Inbox
	secretDB := global.IL.Secret

	Relay = khatru.NewRelay()
	Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Inbox.HTTPBasePath

	Relay.ManagementAPI.ChangeRelayName = changeInboxRelayNameHandler
	Relay.ManagementAPI.ChangeRelayDescription = changeInboxRelayDescriptionHandler
	Relay.ManagementAPI.ChangeRelayIcon = changeInboxRelayIconHandler
	Relay.ManagementAPI.ListBannedPubKeys = listBannedPubkeysHandler
	Relay.ManagementAPI.BanPubKey = banPubkeyHandler
	Relay.ManagementAPI.AllowPubKey = allowPubkeyHandler

	// use dual layer store
	Relay.UseEventstore(&dualLayerStore{
		normalDB: normalDB,
		secretDB: secretDB,
	}, 500)

	pk := global.Settings.RelayInternalSecretKey.Public()
	Relay.Info.Self = &pk
	Relay.Info.PubKey = &pk

	Relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
		rejectFilter,
	)
	Relay.OnEvent = rejectEvent
	Relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)
	Relay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		info.Name = global.Settings.Inbox.Name
		if info.Name == "" {
			info.Name = global.Settings.RelayName + " - inbox"
		}
		info.Description = global.Settings.Inbox.Description
		if info.Description == "" {
			info.Description = "filtered notifications for relay members using a unified web of trust"
		}
		info.Icon = global.Settings.Inbox.Icon
		if info.Icon == "" {
			info.Icon = global.Settings.RelayIcon
		}
		info.Contact = global.Settings.RelayContact
		info.Software = "https://github.com/fiatjaf/pyramid"

		return info
	}

	Relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		inboxPage(loggedUser).Render(r.Context(), w)
	})

	Relay.Router().HandleFunc("POST /disable", disableHandler)

	// compute aggregated WoT in background every 48h
	go func() {
		ctx := context.Background()
		time.Sleep(time.Minute * 2)
		for {
			wot, err := computeAggregatedWoT(ctx)
			if err != nil {
				nostr.InfoLogger.Println("failed to compute aggregated WoT:", err)
			}
			aggregatedWoT = wot
			nostr.InfoLogger.Printf("computed aggregated WoT with %d entries", wot.Items)
			time.Sleep(48 * time.Hour)
		}
	}()
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Inbox.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/inbox/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Inbox.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/", 302)
}

func changeInboxRelayNameHandler(ctx context.Context, name string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Inbox.Name = name
	return global.SaveUserSettings()
}

func changeInboxRelayDescriptionHandler(ctx context.Context, description string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Inbox.Description = description
	return global.SaveUserSettings()
}

func changeInboxRelayIconHandler(ctx context.Context, icon string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Inbox.Icon = icon
	return global.SaveUserSettings()
}

func listBannedPubkeysHandler(ctx context.Context) ([]nip86.PubKeyReason, error) {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return nil, fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return nil, fmt.Errorf("unauthorized")
	}

	var result []nip86.PubKeyReason
	for _, pubkey := range global.Settings.Inbox.SpecificallyBlocked {
		result = append(result, nip86.PubKeyReason{
			PubKey: pubkey,
			Reason: "",
		})
	}
	return result, nil
}

func banPubkeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	// check if already banned
	for _, p := range global.Settings.Inbox.SpecificallyBlocked {
		if p == pubkey {
			return nil // already banned
		}
	}

	global.Settings.Inbox.SpecificallyBlocked = append(global.Settings.Inbox.SpecificallyBlocked, pubkey)
	return global.SaveUserSettings()
}

func allowPubkeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	// remove from list
	var newList []nostr.PubKey
	for _, p := range global.Settings.Inbox.SpecificallyBlocked {
		if p != pubkey {
			newList = append(newList, p)
		}
	}
	global.Settings.Inbox.SpecificallyBlocked = newList
	return global.SaveUserSettings()
}
