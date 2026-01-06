package grasp

import (
	"net/http"
	"os"
	"path/filepath"

	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/grasp"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log       = global.Log.With().Str("relay", "grasp").Logger()
	Handler   = &MuxHandler{}
	repoDir   string
	hostRelay *khatru.Relay
)

func Init(relay *khatru.Relay) {
	hostRelay = relay
	repoDir = filepath.Join(global.S.DataPath, "grasp-repos")

	if !global.Settings.Grasp.Enabled {
		// relay disabled
		setupDisabled()
	} else {
		// relay enabled
		setupEnabled()
	}
}

func setupDisabled() {
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /grasp/enable", enableHandler)
	Handler.mux.HandleFunc("/grasp/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		graspPage(loggedUser).Render(r.Context(), w)
	})
}

func setupEnabled() {
	// create repository directory
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		log.Error().Err(err).Msg("failed to create repository directory")
		return
	}

	// set up grasp server
	grasp.New(hostRelay, repoDir)

	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /grasp/disable", disableHandler)
	Handler.mux.HandleFunc("/grasp/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		// fetch fresh repository list on each request
		graspPage(loggedUser).Render(r.Context(), w)
	})
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Grasp.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/grasp/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Grasp.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/grasp/", 302)
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}
