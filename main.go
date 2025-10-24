package main

import (
	"context"
	"embed"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"
	"fiatjaf.com/nostr/nip11"
	"fiatjaf.com/nostr/nip29"
	"fiatjaf.com/nostr/sdk"
	"golang.org/x/sync/errgroup"

	"github.com/fiatjaf/pyramid/favorites"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/groups"
	"github.com/fiatjaf/pyramid/inbox"
	"github.com/fiatjaf/pyramid/internal"
	"github.com/fiatjaf/pyramid/popular"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/fiatjaf/pyramid/uppermost"
)

var log = global.Log

//go:embed static/*
var static embed.FS

func setupCheckMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/setup/") {
			next.ServeHTTP(w, r)
			return
		}

		if global.Settings.Domain == "" {
			http.Redirect(w, r, "/setup/domain", 302)
			return
		}

		if !pyramid.HasRootUsers() {
			http.Redirect(w, r, "/setup/root", 302)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	if err := global.Init(); err != nil {
		log.Fatal().Err(err).Msg("couldn't initialize")
		return
	}
	defer global.End()

	// load pyramid early for setup checks
	if err := pyramid.LoadManagement(); err != nil {
		log.Fatal().Err(err).Msg("failed to load members")
		return
	}

	root := khatru.NewRouter()
	root.Relay.ServiceURL = "wss://" + global.Settings.Domain

	// enable negentropy
	root.Relay.Negentropy = true

	global.Nostr = sdk.NewSystem()
	global.Nostr.Store = global.IL.System

	// init relays
	internalRelay := internal.NewRelay(global.IL.Internal)
	favoritesRelay := favorites.NewRelay(global.IL.Favorites)
	uppermostRelay := uppermost.NewRelay(global.IL.Uppermost)
	popularRelay := popular.NewRelay(global.IL.Popular)
	groupsRelay, groupsHttpHandler := groups.NewRelay(global.IL.Groups)
	inboxRelay := inbox.NewRelay(global.IL.Inbox, global.IL.Secret)

	// init main relay
	root.Relay.Info.Name = global.Settings.RelayName

	// use the first root we find here, whatever
	for member, invitedBy := range pyramid.Members {
		if slices.Contains(invitedBy, nostr.ZeroPK) {
			root.Relay.Info.PubKey = &member
			break
		}
	}
	root.Relay.Info.Description = global.Settings.RelayDescription
	root.Relay.Info.Contact = global.Settings.RelayContact
	root.Relay.Info.Icon = global.Settings.RelayIcon
	root.Relay.Info.Limitation = &nip11.RelayLimitationDocument{
		RestrictedWrites: true,
	}
	root.Relay.Info.Software = "https://github.com/fiatjaf/pyramid"

	relay := khatru.NewRelay()
	relay.UseEventstore(global.IL.Main, 500)
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

	relay.OnEventSaved = processReactions

	root.Relay.ManagementAPI.AllowPubKey = allowPubKeyHandler
	root.Relay.ManagementAPI.BanPubKey = banPubKeyHandler
	root.Relay.ManagementAPI.ListAllowedPubKeys = listAllowedPubKeysHandler

	// setup routes (no auth required)
	root.Relay.Router().HandleFunc("/setup/domain", domainSetupHandler)
	root.Relay.Router().HandleFunc("/setup/root", rootUserSetupHandler)

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
	mux.Handle("/uppermost/", http.StripPrefix("/uppermost", uppermostRelay))
	mux.Handle("/popular/", http.StripPrefix("/popular", popularRelay))
	mux.Handle("/inbox/", http.StripPrefix("/inbox", inboxRelay))
	mux.Handle("/", root)

	server := &http.Server{Addr: ":" + global.S.Port, Handler: setupCheckMiddleware(mux)}
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
