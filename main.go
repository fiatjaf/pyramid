package main

import (
	"context"
	"embed"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"
	"fiatjaf.com/nostr/nip11"
	"fiatjaf.com/nostr/nip29"
	"fiatjaf.com/nostr/sdk"
	"golang.org/x/sync/errgroup"

	"github.com/fiatjaf/pyramid/favorites"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/groups"
	"github.com/fiatjaf/pyramid/internal"
	"github.com/fiatjaf/pyramid/whitelist"
)

var log = global.Log

//go:embed static/*
var static embed.FS

func main() {
	if err := global.Init(); err != nil {
		log.Fatal().Err(err).Msg("couldn't initialize")
		return
	}

	if err := os.MkdirAll(global.S.DataPath, 0755); err != nil {
		log.Fatal().Err(err).Str("dir", global.S.DataPath).Msg("failed to create data directory")
		return
	}

	root := khatru.NewRouter()
	root.Relay.ServiceURL = "wss://" + global.S.Domain

	// enable negentropy
	root.Relay.Negentropy = true

	// load db
	global.MMMM = &mmm.MultiMmapManager{
		Logger: &log,
		Dir:    global.S.DataPath,
	}
	if err := global.MMMM.Init(); err != nil {
		log.Fatal().Err(err).Msg("failed to setup mmm")
		return
	}
	defer global.MMMM.Close()

	var db *mmm.IndexingLayer
	if layer, err := global.MMMM.EnsureLayer("main"); err != nil {
		log.Fatal().Err(err).Msg("failed to setup main indexing layer")
		return
	} else {
		db = layer
	}

	global.Nostr = sdk.NewSystem()
	global.Nostr.Store = db

	// setup additional indexing layers
	var internalDB *mmm.IndexingLayer
	if layer, err := global.MMMM.EnsureLayer("internal"); err != nil {
		log.Fatal().Err(err).Msg("failed to setup internal indexing layer")
		return
	} else {
		internalDB = layer
	}

	var groupsDB *mmm.IndexingLayer
	if layer, err := global.MMMM.EnsureLayer("groups"); err != nil {
		log.Fatal().Err(err).Msg("failed to setup groups indexing layer")
		return
	} else {
		groupsDB = layer
	}

	var favoritesDB *mmm.IndexingLayer
	if layer, err := global.MMMM.EnsureLayer("favorites"); err != nil {
		log.Fatal().Err(err).Msg("failed to setup favorites indexing layer")
		return
	} else {
		favoritesDB = layer
	}

	// init relays
	internalRelay := internal.NewRelay(internalDB)
	favoritesRelay := favorites.NewRelay(favoritesDB)
	groupsRelay, groupsHttpHandler := groups.NewRelay(groupsDB)

	// init main relay
	root.Relay.Info.Name = global.Settings.RelayName

	if pk, err := nostr.PubKeyFromHex(global.S.RelayPubkey); err != nil {
		log.Fatal().Err(err).Str("value", global.S.RelayPubkey).Msg("invalid relay main pubkey")
	} else {
		root.Relay.Info.PubKey = &pk
		global.Master = pk
	}
	root.Relay.Info.Description = global.Settings.RelayDescription
	root.Relay.Info.Contact = global.Settings.RelayContact
	root.Relay.Info.Icon = global.Settings.RelayIcon
	root.Relay.Info.Limitation = &nip11.RelayLimitationDocument{
		RestrictedWrites: true,
	}
	root.Relay.Info.Software = "https://github.com/fiatjaf/pyramid"

	relay := khatru.NewRelay()
	relay.UseEventstore(db, 500)
	relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
	)
	relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)

	relay.OnEvent = policies.SeqEvent(
		policies.PreventLargeContent(10000),
		policies.PreventTooManyIndexableTags(9, []nostr.Kind{3}, nil),
		policies.PreventTooManyIndexableTags(1200, nil, []nostr.Kind{3}),
		policies.RestrictToSpecifiedKinds(true, supportedKinds...),
		policies.RejectUnprefixedNostrReferences,
		basicRejectionLogic,
	)

	root.Relay.ManagementAPI.AllowPubKey = allowPubKeyHandler
	root.Relay.ManagementAPI.BanPubKey = banPubKeyHandler
	root.Relay.ManagementAPI.ListAllowedPubKeys = listAllowedPubKeysHandler

	// load users registry
	if err := whitelist.LoadManagement(); err != nil {
		log.Fatal().Err(err).Msg("failed to load whitelist")
		return
	}

	// http routes
	root.Relay.Router().HandleFunc("/action", actionHandler)
	root.Relay.Router().HandleFunc("/cleanup", cleanupStuffFromExcludedUsersHandler)
	root.Relay.Router().HandleFunc("/reports", reportsViewerHandler)
	root.Relay.Router().HandleFunc("/settings", settingsHandler)
	root.Relay.Router().HandleFunc("POST /upload-icon", uploadIconHandler)
	root.Relay.Router().HandleFunc("POST /enable-groups", enableGroupsHandler)
	root.Relay.Router().HandleFunc("/icon.png", iconHandler)
	root.Relay.Router().HandleFunc("/icon.jpg", iconHandler)
	root.Relay.Router().HandleFunc("/forum/", forumHandler)
	root.Relay.Router().Handle("/static/", http.FileServer(http.FS(static)))
	root.Relay.Router().HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		if global.Settings.RelayIcon != "" {
			http.Redirect(w, r, global.Settings.RelayIcon, 302)
		} else {
			http.Redirect(w, r, "/static/icon.png", 302)
		}
	})
	root.Relay.Router().HandleFunc("/{$}", inviteTreeHandler)

	// route nostr requests for nip29 groups to the groupsRelay directly
	root.Route().
		Event(func(evt *nostr.Event) bool { return evt.Tags.Find("h") != nil }).
		Req(func(filter nostr.Filter) bool {
			if filter.Tags["h"] != nil {
				return true
			}

			for _, kind := range filter.Kinds {
				if slices.Contains(nip29.MetadataEventKinds, kind) {
					return true
				}
			}

			return false
		}).
		Relay(groupsRelay)
	// (all the others go to the root relay)
	root.Route().
		Relay(relay)

	log.Info().Msg("running on http://0.0.0.0:" + global.S.Port)

	mux := http.NewServeMux()
	mux.Handle("/internal/", http.StripPrefix("/internal", internalRelay))
	mux.Handle("/groups/", http.StripPrefix("/groups", groupsHttpHandler))
	mux.Handle("/favorites/", http.StripPrefix("/favorites", favoritesRelay))
	mux.Handle("/", root)

	server := &http.Server{Addr: ":" + global.S.Port, Handler: mux}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	g.Go(server.ListenAndServe)
	g.Go(func() error {
		<-ctx.Done()
		return server.Shutdown(context.Background())
	})
	if err := g.Wait(); err != nil {
		log.Debug().Err(err).Msg("exit reason")
	}
}
