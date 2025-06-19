package main

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fiatjaf.com/nostr/eventstore/lmdb"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"
	"github.com/kelseyhightower/envconfig"
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip11"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

type Settings struct {
	Port             string `envconfig:"PORT" default:"3334"`
	Domain           string `envconfig:"DOMAIN" required:"true"`
	RelayName        string `envconfig:"RELAY_NAME" required:"true"`
	RelayPubkey      string `envconfig:"RELAY_PUBKEY" required:"true"`
	RelayDescription string `envconfig:"RELAY_DESCRIPTION"`
	RelayContact     string `envconfig:"RELAY_CONTACT"`
	RelayIcon        string `envconfig:"RELAY_ICON"`
	DatabasePath     string `envconfig:"DATABASE_PATH" default:"./db"`
	UserdataPath     string `envconfig:"USERDATA_PATH" default:"./users.json"`

	MaxInvitesPerPerson int `envconfig:"MAX_INVITES_PER_PERSON" default:"3"`
}

var (
	s  Settings
	db = &lmdb.LMDBBackend{
		MaxLimit:           500,
		MaxLimitNegentropy: 999999,
		EnableHLLCacheFor: func(kind int) (useCache bool, skipSavingActualEvent bool) {
			switch kind {
			case 7:
				return true, true
			case 1111:
				return true, false
			default:
				return false, false
			}
		},
	}
	log       = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	whitelist = make(Whitelist)
	relay     = khatru.NewRelay()
)

//go:embed static/*
var static embed.FS

func main() {
	err := envconfig.Process("", &s)
	if err != nil {
		log.Fatal().Err(err).Msg("couldn't process envconfig")
		return
	}

	relay.ServiceURL = "wss://" + s.Domain

	// enable negentropy
	relay.Negentropy = true

	// load db
	db.Path = s.DatabasePath
	if err := db.Init(); err != nil {
		log.Fatal().Err(err).Msg("failed to initialize database")
		return
	}
	defer db.Close()
	log.Debug().Str("path", db.Path).Msg("initialized database")

	// init relay
	relay.Info.Name = s.RelayName
	relay.Info.PubKey = s.RelayPubkey
	relay.Info.Description = s.RelayDescription
	relay.Info.Contact = s.RelayContact
	relay.Info.Icon = s.RelayIcon
	relay.Info.Limitation = &nip11.RelayLimitationDocument{
		RestrictedWrites: true,
	}
	relay.Info.Software = "https://github.com/fiatjaf/pyramid"

	relay.RejectFilter = append(relay.RejectFilter,
		policies.NoComplexFilters,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
	)
	relay.RejectConnection = append(relay.RejectConnection,
		policies.ConnectionRateLimiter(1, time.Minute*5, 100),
	)

	relay.StoreEvent = append(relay.StoreEvent, db.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents, db.QueryEvents)
	relay.CountEventsHLL = append(relay.CountEventsHLL, db.CountEventsHLL)
	relay.ReplaceEvent = append(relay.ReplaceEvent, db.ReplaceEvent)
	relay.DeleteEvent = append(relay.DeleteEvent, db.DeleteEvent)
	relay.RejectEvent = append(relay.RejectEvent,
		policies.PreventLargeTags(100),
		policies.PreventTooManyIndexableTags(9, []int{3}, nil),
		policies.PreventTooManyIndexableTags(1200, nil, []int{3}),
		policies.RestrictToSpecifiedKinds(true, supportedKinds...),
		rejectEventsFromUsersNotInWhitelist,
		validateAndFilterReports,
	)
	relay.OverwriteFilter = append(relay.OverwriteFilter,
		policies.RemoveAllButKinds(supportedKinds...),
		removeAuthorsNotWhitelisted,
	)
	relay.RejectFilter = append(relay.RejectFilter,
		policies.NoSearchQueries,
	)

	relay.ManagementAPI.AllowPubKey = allowPubKeyHandler
	relay.ManagementAPI.BanPubKey = banPubKeyHandler
	relay.ManagementAPI.ListAllowedPubKeys = listAllowedPubKeysHandler

	// load users registry
	if err := loadWhitelist(); err != nil {
		log.Fatal().Err(err).Msg("failed to load whitelist")
		return
	}

	// http routes
	relay.Router().HandleFunc("/add-to-whitelist", addToWhitelistHandler)
	relay.Router().HandleFunc("/remove-from-whitelist", removeFromWhitelistHandler)
	relay.Router().HandleFunc("/cleanup", cleanupStuffFromExcludedUsersHandler)
	relay.Router().HandleFunc("/reports", reportsViewerHandler)
	relay.Router().HandleFunc("/forum/", forumHandler)
	relay.Router().Handle("/static/", http.FileServer(http.FS(static)))
	relay.Router().HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		if s.RelayIcon != "" {
			http.Redirect(w, r, s.RelayIcon, 302)
		} else {
			http.Redirect(w, r, "/static/icon.png", 302)
		}
	})
	relay.Router().HandleFunc("/", inviteTreeHandler)

	log.Info().Msg("running on http://0.0.0.0:" + s.Port)

	server := &http.Server{Addr: ":" + s.Port, Handler: relay}
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

func getLoggedUser(r *http.Request) string {
	if cookie, _ := r.Cookie("nip98"); cookie != nil {
		if evtj, err := url.QueryUnescape(cookie.Value); err == nil {
			var evt nostr.Event
			if err := json.Unmarshal([]byte(evtj), &evt); err == nil {
				if tag := evt.Tags.Find("domain"); tag != nil && tag[1] == s.Domain {
					if ok, _ := evt.CheckSignature(); ok {
						return evt.PubKey
					}
				}
			}
		}
	}
	return ""
}
