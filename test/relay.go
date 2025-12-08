package test

import (
	"context"
	"fmt"
	"net/http"
	"time"

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

// StartRelay initializes and starts the relay server in a goroutine for testing
func StartRelay(ctx context.Context) error {
	if err := global.Init(); err != nil {
		return fmt.Errorf("couldn't initialize: %w", err)
	}

	if err := pyramid.LoadManagement(); err != nil {
		return fmt.Errorf("failed to load members: %w", err)
	}

	favorites.Init()
	groups.Init()
	inbox.Init()
	internal.Init()
	moderated.Init()
	popular.Init()
	uppermost.Init()

	// start server in background
	go func() {
		// simplified version of run() from main.go for testing
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

		server := &http.Server{
			Addr:    global.S.Host + ":" + global.S.Port,
			Handler: mux,
		}

		go func() {
			<-ctx.Done()
			server.Shutdown(context.Background())
		}()

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			global.Log.Error().Err(err).Msg("server error")
		}
	}()

	// wait for server to start
	time.Sleep(100 * time.Millisecond)

	return nil
}
