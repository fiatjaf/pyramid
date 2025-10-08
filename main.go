package main

import (
	"context"
	"embed"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/lmdb"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"
	"fiatjaf.com/nostr/nip11"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/whitelist"
)

var (
	log   = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	db    *lmdb.LMDBBackend
	relay = khatru.NewRelay()
)

//go:embed static/*
var static embed.FS

func main() {
	err := envconfig.Process("", &global.S)
	if err != nil {
		log.Fatal().Err(err).Msg("couldn't process envconfig")
		return
	}

	if err := os.MkdirAll(global.S.DataPath, 0755); err != nil {
		log.Fatal().Err(err).Str("dir", global.S.DataPath).Msg("failed to create data directory")
		return
	}

	relay.ServiceURL = "wss://" + global.S.Domain

	// enable negentropy
	relay.Negentropy = true

	// load db
	db = &lmdb.LMDBBackend{Path: global.S.DataPath}
	if err := db.Init(); err != nil {
		log.Fatal().Err(err).Msg("failed to initialize database")
		return
	}
	defer db.Close()
	log.Debug().Str("path", db.Path).Msg("initialized database")

	// init relay
	relay.Info.Name = global.S.RelayName

	if pk, err := nostr.PubKeyFromHex(global.S.RelayPubkey); err != nil {
		log.Fatal().Err(err).Str("value", global.S.RelayPubkey).Msg("invalid relay main pubkey")
	} else {
		relay.Info.PubKey = &pk
		global.Master = pk
	}
	relay.Info.Description = global.S.RelayDescription
	relay.Info.Contact = global.S.RelayContact
	relay.Info.Icon = global.S.RelayIcon
	relay.Info.Limitation = &nip11.RelayLimitationDocument{
		RestrictedWrites: true,
	}
	relay.Info.Software = "https://github.com/fiatjaf/pyramid"

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
		rejectEventsFromUsersNotInWhitelist,
		validateAndFilterReports,
	)

	relay.ManagementAPI.AllowPubKey = allowPubKeyHandler
	relay.ManagementAPI.BanPubKey = banPubKeyHandler
	relay.ManagementAPI.ListAllowedPubKeys = listAllowedPubKeysHandler

	// load users registry
	if err := whitelist.LoadManagement(); err != nil {
		log.Fatal().Err(err).Msg("failed to load whitelist")
		return
	}

	// http routes
	relay.Router().HandleFunc("/action", actionHandler)
	relay.Router().HandleFunc("/cleanup", cleanupStuffFromExcludedUsersHandler)
	relay.Router().HandleFunc("/reports", reportsViewerHandler)
	relay.Router().HandleFunc("/forum/", forumHandler)
	relay.Router().Handle("/static/", http.FileServer(http.FS(static)))
	relay.Router().HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		if global.S.RelayIcon != "" {
			http.Redirect(w, r, global.S.RelayIcon, 302)
		} else {
			http.Redirect(w, r, "/static/icon.png", 302)
		}
	})
	relay.Router().HandleFunc("/{$}", inviteTreeHandler)

	log.Info().Msg("running on http://0.0.0.0:" + global.S.Port)

	server := &http.Server{Addr: ":" + global.S.Port, Handler: relay}
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
