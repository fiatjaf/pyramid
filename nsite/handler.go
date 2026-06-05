package nsite

import (
	"errors"
	"net/http"
	"strings"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log             = global.Log.With().Str("service", "nsite").Logger()
	Handler         = &MuxHandler{}
	errSiteNotFound = errors.New("site not found")
)

func Init() {
	if !global.Settings.Nsite.Enabled {
		setupDisabled()
	} else {
		setupEnabled()
	}
}

func setupDisabled() {
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /nsite/enable", enableHandler)
	Handler.mux.HandleFunc("/nsite/", pageHandler)
}

func setupEnabled() {
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /nsite/disable", disableHandler)
	Handler.mux.HandleFunc("/nsite/", pageHandler)
}

func MatchesHost(host string) bool {
	if global.Settings.Nsite.Enabled {
		if baseDomain := strings.Trim(strings.ToLower(global.Settings.Nsite.Domain), "."); baseDomain != "" {
			baseDomainWithoutPort := strings.Split(baseDomain, ":")[0]
			if strings.HasSuffix(host, "."+baseDomainWithoutPort) {
				if label := strings.TrimSuffix(host, "."+baseDomainWithoutPort); label != "" && !strings.Contains(label, ".") {
					return true
				}
			}
		}
	}

	return false
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	nsitePage(loggedUser).Render(r.Context(), w)
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Nsite.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/nsite/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Nsite.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/nsite/", 302)
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}
