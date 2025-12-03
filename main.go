package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"iter"
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
	"github.com/fiatjaf/pyramid/grasp"
	"github.com/fiatjaf/pyramid/groups"
	"github.com/fiatjaf/pyramid/inbox"
	"github.com/fiatjaf/pyramid/internal"
	"github.com/fiatjaf/pyramid/moderated"
	"github.com/fiatjaf/pyramid/popular"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/fiatjaf/pyramid/uppermost"
)

var (
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

	pyramid.AbsoluteKey = global.Settings.RelayInternalSecretKey.Public()

	if err := pyramid.LoadManagement(); err != nil {
		log.Fatal().Err(err).Msg("failed to load members")
		return
	}

	// init main relay
	relay = khatru.NewRelay()
	relay.Info.Name = "main" // for debugging purposes
	relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain
	relay.Negentropy = true

	// init sdk
	global.Nostr = sdk.NewSystem()
	global.Nostr.Store = global.IL.System

	// init setup routes (no auth required) -- for one-time use only
	if global.Settings.Domain == "" {
		relay.Router().HandleFunc("/setup/domain", domainSetupHandler)
	}
	if !pyramid.HasRootUsers() {
		relay.Router().HandleFunc("/setup/root", rootUserSetupHandler)
	}

	// init basic http routes
	relay.Router().HandleFunc("/action", actionHandler)
	relay.Router().HandleFunc("/cleanup", cleanupStuffFromExcludedUsersHandler)
	relay.Router().HandleFunc("/reports", reportsViewerHandler)
	relay.Router().HandleFunc("/settings", settingsHandler)
	relay.Router().HandleFunc("/icon/{relayId}", iconHandler)
	relay.Router().HandleFunc("/forum/", forumHandler)
	relay.Router().Handle("/static/", http.FileServer(http.FS(static)))
	relay.Router().HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		if global.Settings.RelayIcon != "" {
			http.Redirect(w, r, global.Settings.RelayIcon, 302)
		} else {
			http.Redirect(w, r, "/static/icon.png", 302)
		}
	})
	relay.Router().HandleFunc("/{$}", inviteTreeHandler)

	// init sub relays
	favorites.Init()
	grasp.Init(relay)
	groups.Init(relay)
	inbox.Init()
	internal.Init()
	moderated.Init()
	popular.Init()
	uppermost.Init()

	// setup main relay hooks and so on
	relay.QueryStored = queryStored
	relay.Count = func(ctx context.Context, filter nostr.Filter) (uint32, error) {
		// ignore groups in this case for now
		return global.IL.Main.CountEvents(filter)
	}
	relay.StoreEvent = func(ctx context.Context, event nostr.Event) error {
		if event.Tags.Find("h") != nil {
			// nip29 logic
			return global.IL.Groups.SaveEvent(event)
		} else {
			// normal logic
			return global.IL.Main.SaveEvent(event)
		}
	}
	relay.ReplaceEvent = func(ctx context.Context, event nostr.Event) error {
		if event.Tags.Find("h") != nil {
			// nip29 logic
			return global.IL.Groups.ReplaceEvent(event)
		} else {
			// normal logic
			return global.IL.Main.ReplaceEvent(event)
		}
	}
	relay.DeleteEvent = func(ctx context.Context, id nostr.ID) error {
		// try to delete from both
		if err := global.IL.Main.DeleteEvent(id); err != nil {
			return err
		}
		if err := global.IL.Groups.DeleteEvent(id); err != nil {
			return err
		}
		return nil
	}

	// do not expire groups stuff, but do expire main stuff
	relay.StartExpirationManager(
		func(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
			return global.IL.Main.QueryEvents(filter, 500)
		},
		func(ctx context.Context, id nostr.ID) error {
			return global.IL.Main.DeleteEvent(id)
		},
	)

	relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
		func(ctx context.Context, filter nostr.Filter) (bool, string) {
			if filter.Tags["h"] != nil {
				// nip29 logic
				if global.Settings.Groups.Enabled {
					return groups.State.RequestAuthWhenNecessary(ctx, filter)
				} else {
					return true, "groups are disabled"
				}
			}

			for _, nip29k := range nip29.MetadataEventKinds {
				if idx := slices.Index(filter.Kinds, nip29k); idx != -1 {
					// nip29 logic
					if global.Settings.Groups.Enabled {
						return groups.State.RequestAuthWhenNecessary(ctx, filter)
					} else {
						return true, "groups are disabled"
					}
				}
			}

			// normal logic
			return rejectInviteRequestsNonAuthed(ctx, filter)
		},
	)
	relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 30)
	relay.OnEvent = func(ctx context.Context, event nostr.Event) (reject bool, msg string) {
		if len(event.Content) > 10_000 {
			return true, "content is too big"
		}

		// we don't allow deleting old messages in groups, so we have to reject here
		if event.Kind == nostr.KindDeletion {
			for e := range event.Tags.FindAll("e") {
				if eid, err := nostr.IDFromHex(e[1]); err == nil {
					for evt := range global.IL.Groups.QueryEvents(nostr.Filter{IDs: []nostr.ID{eid}}, 1) {
						if evt.CreatedAt < nostr.Now()-60*60*2 /* 2 hours */ {
							return true, "can't delete very old group message"
						}
					}
				}
			}
		}

		if event.Tags.Find("h") != nil {
			// nip29 logic
			if global.Settings.Groups.Enabled {
				return groups.State.RejectEvent(ctx, event)
			} else {
				return true, "groups are disabled"
			}
		} else {
			// normal logic
			return policies.SeqEvent(
				policies.PreventTooManyIndexableTags(9, []nostr.Kind{3}, nil),
				policies.PreventTooManyIndexableTags(1200, nil, []nostr.Kind{3}),
				policies.RestrictToSpecifiedKinds(true, supportedKinds...),
				policies.RejectUnprefixedNostrReferences,
				basicRejectionLogic,
			)(ctx, event)
		}
	}

	relay.OnEventSaved = func(ctx context.Context, event nostr.Event) {
		if h := event.Tags.Find("h"); h != nil {
			// nip29 logic
			groups.State.ProcessEvent(ctx, event)
			return
		}

		// normal logic
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

	relay.Info.SupportedNIPs = append(relay.Info.SupportedNIPs, 43)
	if global.Settings.Groups.Enabled {
		relay.Info.SupportedNIPs = append(relay.Info.SupportedNIPs, 29)
	}
	relay.ManagementAPI.AllowPubKey = allowPubKeyHandler
	relay.ManagementAPI.BanPubKey = banPubKeyHandler
	relay.ManagementAPI.ListAllowedPubKeys = listAllowedPubKeysHandler
	relay.ManagementAPI.ChangeRelayName = changeRelayNameHandler
	relay.ManagementAPI.ChangeRelayDescription = changeRelayDescriptionHandler
	relay.ManagementAPI.ChangeRelayIcon = changeRelayIconHandler
	relay.ManagementAPI.ListBlockedIPs = listBlockedIPsHandler
	relay.ManagementAPI.BlockIP = blockIPHandler
	relay.ManagementAPI.UnblockIP = unblockIPHandler
	relay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		fmt.Println("NIP11 request:", r.Header.Get("User-Agent"), r.Header.Get("X-Forwarded-For"), r.Header.Get("Referer"))

		if strings.Contains(r.Header.Get("User-Agent"), "aiohttp") {
			if idx := slices.Index(info.SupportedNIPs, 29); idx != -1 {
				info.SupportedNIPs[idx] = info.SupportedNIPs[len(info.SupportedNIPs)-1]
				info.SupportedNIPs = info.SupportedNIPs[0 : len(info.SupportedNIPs)-1]
			}
		}

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

	mux.Handle("/grasp/",
		http.StripPrefix("/grasp", grasp.Handler))
	mux.Handle("/grasp",
		http.StripPrefix("/grasp", grasp.Handler))

	mux.Handle("/groups/",
		http.StripPrefix("/groups", groups.Handler))
	mux.Handle("/groups",
		http.StripPrefix("/groups", groups.Handler))

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

	mux.Handle("/", relay)

	g, ctx := errgroup.WithContext(ctx)

	// copy here as we'll modify it
	port := global.S.Port

	if port == "443" {
		// if we don't have a domain name we'll listen only on port 80 without doing the autocert dance yet
		if global.Settings.Domain == "" {
			log.Info().Msg("no domain setup yet, running only on port 80 for now")
			port = "80"
		}
	}

	server := &http.Server{
		Addr:    global.S.Host + ":" + port,
		Handler: ipBlockMiddleware(setupCheckMiddleware(mux)),
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	if port == "443" {
		manager := &autocert.Manager{
			Prompt:     func(_ string) bool { return true },
			HostPolicy: autocert.HostWhitelist(global.Settings.Domain),
			Cache:      autocert.DirCache("certs"),
		}

		// HTTP server on 80 for ACME challenges and user access
		httpServer := &http.Server{
			Addr:    global.S.Host + ":80",
			Handler: manager.HTTPHandler(mux),
			BaseContext: func(_ net.Listener) context.Context {
				return ctx
			},
		}
		g.Go(func() error { return httpServer.ListenAndServe() })

		// HTTPS server on 443
		httpsServer := &http.Server{
			Addr:    global.S.Host + ":443",
			Handler: ipBlockMiddleware(mux),
			BaseContext: func(_ net.Listener) context.Context {
				return ctx
			},
		}
		httpsServer.TLSConfig = manager.TLSConfig()

		g.Go(func() error { return httpsServer.ListenAndServeTLS("", "") })
		log.Info().Msg("running on https://" + global.S.Host + ":443 and http://" + global.S.Host + ":80")
		g.Go(func() error {
			<-ctx.Done()
			httpsServer.Shutdown(context.Background())
			httpServer.Shutdown(context.Background())
			return nil
		})
	} else {
		g.Go(server.ListenAndServe)
		log.Info().Msg("running on http://" + global.S.Host + ":" + port)

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
	}

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
