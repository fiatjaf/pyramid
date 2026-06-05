package nsite

import (
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"

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

func resolveSite(host string) (nostr.PubKey, string, error) {
	domain := strings.Trim(strings.ToLower(global.Settings.Nsite.Domain), ".")
	if host == domain {
		return nostr.ZeroPK, "", errSiteNotFound
	}

	label := strings.TrimSuffix(host, "."+domain)
	label = strings.TrimSuffix(label, ".")
	if label == "" || strings.Contains(label, ".") {
		return nostr.ZeroPK, "", errSiteNotFound
	}

	if prefix, value, err := nip19.Decode(label); err == nil && prefix == "npub" {
		if pubkey, ok := value.(nostr.PubKey); ok {
			return pubkey, "", nil
		}
	}

	pubkey, err := decodePubkeyB36(label[:50])
	if err != nil {
		return nostr.ZeroPK, "", errSiteNotFound
	}
	return pubkey, label[50:], nil
}

func decodePubkeyB36(s string) (nostr.PubKey, error) {
	n, ok := new(big.Int).SetString(s, 36)
	if !ok {
		return nostr.ZeroPK, fmt.Errorf("invalid base36 pubkey")
	}
	b := n.Bytes()
	if len(b) > 32 {
		return nostr.ZeroPK, fmt.Errorf("base36 pubkey too large")
	}
	var pubkey nostr.PubKey
	copy(pubkey[32-len(b):], b)
	return pubkey, nil
}
