package main

import (
	"context"
	"embed"
	"errors"
	"net"
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
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/sync/errgroup"

	"github.com/fiatjaf/pyramid/favorites"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/groups"
	"github.com/fiatjaf/pyramid/inbox"
	"github.com/fiatjaf/pyramid/internal"
	"github.com/fiatjaf/pyramid/moderated"
	"github.com/fiatjaf/pyramid/popular"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/fiatjaf/pyramid/uppermost"
)

var (
	root  *khatru.Router
	relay *khatru.Relay
	log   = global.Log
)

//go:embed static/*
var static embed.FS

func main() {
	if err := global.Init(); err != nil {
		log.Fatal().Err(err).Msg("couldn't initialize")
		return
	}
	defer global.End()

	if err := pyramid.LoadManagement(); err != nil {
		log.Fatal().Err(err).Msg("failed to load members")
		return
	}

	favorites.Init()
	groups.Init()
	inbox.Init()
	internal.Init()
	moderated.Init()
	popular.Init()
	uppermost.Init()

	root = khatru.NewRouter()

	global.Nostr = sdk.NewSystem()
	global.Nostr.Store = global.IL.System

	// init main relay
	relay = khatru.NewRelay()
	relay.Info.Name = "main" // for debugging purposes
	relay.Negentropy = true

	relay.UseEventstore(global.IL.Main, 500)
	relay.QueryStored = query
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
	relay.OnEventSaved = func(ctx context.Context, event nostr.Event) {
		switch event.Kind {
		case 6, 7, 9321, 9735, 9802, 1, 1111:
			processReactions(ctx, event)
		case 0, 3, 10019:
			global.IL.System.SaveEvent(event)
		}
	}
	relay.OnEphemeralEvent = func(ctx context.Context, event nostr.Event) {
		switch event.Kind {
		case 28934:
			processJoinRequest(ctx, event)
		case 28936:
			processLeaveRequest(ctx, event)
		}
	}

	relay.OnConnect = onConnect
	relay.PreventBroadcast = preventBroadcast

	root.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain
	root.Relay.Info.Name = "root-router" // for debugging purposes, will be overwritten
	root.Relay.Negentropy = true
	root.Relay.ManagementAPI.AllowPubKey = allowPubKeyHandler
	root.Relay.ManagementAPI.BanPubKey = banPubKeyHandler
	root.Relay.ManagementAPI.ListAllowedPubKeys = listAllowedPubKeysHandler
	root.Relay.ManagementAPI.ChangeRelayName = changeRelayNameHandler
	root.Relay.ManagementAPI.ChangeRelayDescription = changeRelayDescriptionHandler
	root.Relay.ManagementAPI.ChangeRelayIcon = changeRelayIconHandler
	root.Relay.ManagementAPI.ListBlockedIPs = listBlockedIPsHandler
	root.Relay.ManagementAPI.BlockIP = blockIPHandler
	root.Relay.ManagementAPI.UnblockIP = unblockIPHandler
	root.Relay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		pk := global.Settings.RelayInternalSecretKey.Public()
		info.Self = &pk
		info.PubKey = &pk

		info.Name = global.Settings.RelayName
		info.Description = global.Settings.RelayDescription
		info.Contact = global.Settings.RelayContact
		info.Icon = global.Settings.RelayIcon
		info.Limitation = &nip11.RelayLimitationDocument{
			RestrictedWrites: true,
		}
		info.Software = "https://github.com/fiatjaf/pyramid"
		return info
	}

	// setup routes (no auth required)
	root.Relay.Router().HandleFunc("/setup/domain", domainSetupHandler)
	root.Relay.Router().HandleFunc("/setup/root", rootUserSetupHandler)

	// http routes
	root.Relay.Router().HandleFunc("/action", actionHandler)
	root.Relay.Router().HandleFunc("/cleanup", cleanupStuffFromExcludedUsersHandler)
	root.Relay.Router().HandleFunc("/reports", reportsViewerHandler)
	root.Relay.Router().HandleFunc("/settings", settingsHandler)
	root.Relay.Router().HandleFunc("/icon/{relayId}", iconHandler)
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
	if global.Settings.Groups.Enabled {
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
			Relay(groups.Relay)
	}
	// (all the others go to the main relay)
	root.Route().
		AnyEvent().
		AnyReq().
		Relay(relay)

	start()
}

var (
	restarting    = errors.New("restarting")
	restartCancel func()
)

func restartSoon() {
	log.Info().Msg("restarting in 1 second")
	time.Sleep(time.Second * 1)
	restartCancel()
}

func start() {
	ctx, cancelWithCause := context.WithCancelCause(context.Background())
	restartCancel = func() { cancelWithCause(restarting) }

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		if context.Cause(ctx) != restarting {
			log.Debug().Err(err).Msg("exit reason")
			return
		}
	}

	// restart if it was a restart request
	if context.Cause(ctx) == restarting {
		start()
	}
}

func run(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.Handle("/"+global.Settings.Internal.HTTPBasePath+"/",
		http.StripPrefix("/"+global.Settings.Internal.HTTPBasePath, internal.Relay))
	mux.Handle("/"+global.Settings.Internal.HTTPBasePath,
		http.StripPrefix("/"+global.Settings.Internal.HTTPBasePath, internal.Relay))

	mux.Handle("/"+global.Settings.Favorites.HTTPBasePath+"/",
		http.StripPrefix("/"+global.Settings.Favorites.HTTPBasePath, favorites.Relay))
	mux.Handle("/"+global.Settings.Favorites.HTTPBasePath,
		http.StripPrefix("/"+global.Settings.Favorites.HTTPBasePath, favorites.Relay))

	mux.Handle("/"+global.Settings.Groups.HTTPBasePath+"/",
		http.StripPrefix("/"+global.Settings.Groups.HTTPBasePath, groups.Relay))
	mux.Handle("/"+global.Settings.Groups.HTTPBasePath,
		http.StripPrefix("/"+global.Settings.Groups.HTTPBasePath, groups.Relay))

	mux.Handle("/"+global.Settings.Inbox.HTTPBasePath+"/",
		http.StripPrefix("/"+global.Settings.Inbox.HTTPBasePath, inbox.Relay))
	mux.Handle("/"+global.Settings.Inbox.HTTPBasePath,
		http.StripPrefix("/"+global.Settings.Inbox.HTTPBasePath, inbox.Relay))

	mux.Handle("/"+global.Settings.Popular.HTTPBasePath+"/",
		http.StripPrefix("/"+global.Settings.Popular.HTTPBasePath, popular.Relay))
	mux.Handle("/"+global.Settings.Popular.HTTPBasePath,
		http.StripPrefix("/"+global.Settings.Popular.HTTPBasePath, popular.Relay))

	mux.Handle("/"+global.Settings.Uppermost.HTTPBasePath+"/",
		http.StripPrefix("/"+global.Settings.Uppermost.HTTPBasePath, uppermost.Relay))
	mux.Handle("/"+global.Settings.Uppermost.HTTPBasePath,
		http.StripPrefix("/"+global.Settings.Uppermost.HTTPBasePath, uppermost.Relay))

	mux.Handle("/"+global.Settings.Moderated.HTTPBasePath+"/",
		http.StripPrefix("/"+global.Settings.Moderated.HTTPBasePath, moderated.Relay))
	mux.Handle("/"+global.Settings.Moderated.HTTPBasePath,
		http.StripPrefix("/"+global.Settings.Moderated.HTTPBasePath, moderated.Relay))

	mux.Handle("/", root)

	server := &http.Server{
		Addr:    global.S.Host + ":" + global.S.Port,
		Handler: ipBlockMiddleware(setupCheckMiddleware(mux)),
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	var listenFunc func() error
	if global.S.Port == "443" {
		manager := &autocert.Manager{
			Prompt:     func(_ string) bool { return true },
			HostPolicy: autocert.HostWhitelist(global.Settings.Domain),
			Cache:      autocert.DirCache("certs"),
		}
		server.TLSConfig = manager.TLSConfig()
		listenFunc = func() error { return server.ListenAndServeTLS("", "") }
		log.Info().Msg("running on https://" + global.S.Host + ":" + global.S.Port)
	} else {
		listenFunc = server.ListenAndServe
		log.Info().Msg("running on http://" + global.S.Host + ":" + global.S.Port)
	}

	g, ctx := errgroup.WithContext(ctx)
	g.Go(listenFunc)
	g.Go(func() error {
		<-ctx.Done()
		if err := server.Shutdown(context.Background()); err != nil {
			return err
		}
		if err := server.Close(); err != nil {
			return err
		}
		return nil
	})
	return g.Wait()
}

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
