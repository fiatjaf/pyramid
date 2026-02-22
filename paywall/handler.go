package paywall

import (
	"context"
	"net/http"

	"fiatjaf.com/nostr/khatru"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log     = global.Log.With().Str("service", "paywall").Logger()
	Handler = &MuxHandler{}
)

func Init(relay *khatru.Relay) {
	if !global.Settings.Paywall.Enabled {
		setupDisabled()
	} else {
		setupEnabled()
	}
}

func setupDisabled() {
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /paywall/enable", enableHandler)
	Handler.mux.HandleFunc("/paywall/", pageHandler)
}

func setupEnabled() {
	// initialize paywall maps for all existing members if paywall is enabled
	if global.Settings.Paywall.Enabled {
		go func() {
			for member := range pyramid.Members.Range {
				RecomputeMemberPaywall(context.Background(), member)
			}
		}()
	}

	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /paywall/disable", disableHandler)
	Handler.mux.HandleFunc("/paywall/", pageHandler)
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	paywallPage(loggedUser).Render(r.Context(), w)
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Paywall.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/paywall/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Paywall.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/paywall/", 302)
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}
