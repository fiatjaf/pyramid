package operator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip11"
	"fiatjaf.com/pomegranate/common"
	"fiatjaf.com/promenade/frost"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/fiatjaf/pyramid/wot"
)

var (
	L       = global.Log.With().Str("module", "operator").Logger()
	Handler = &MuxHandler{}
)

func Init(relay *khatru.Relay) {
	L.Debug().Msg("initializing operator service")

	if !global.Settings.Operator.Enabled {
		setupDisabled()
	} else {
		SetupEnabled()
	}
}

func setupDisabled() {
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /po/enable", enableHandler)
	Handler.mux.HandleFunc("/po/", pageHandler)
	L.Debug().Msg("operator service disabled")
}

func SetupEnabled() {
	googleConfig := &oauth2.Config{
		ClientID:     global.Settings.Operator.GoogleClientID,
		ClientSecret: global.Settings.Operator.GoogleClientSecret,
		RedirectURL:  global.Settings.HTTPScheme() + global.Settings.Domain + "/po/callback/google",
		Scopes: []string{
			"openid",
			"email",
		},
		Endpoint: google.Endpoint,
	}

	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /po/disable", disableHandler)
	Handler.mux.HandleFunc("/po/action/google", common.HandleGoogleLogin(googleConfig))
	Handler.mux.HandleFunc("/po/recover/google", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/po/action/google?intent=recover", http.StatusFound)
	})
	Handler.mux.HandleFunc("/po/erase/google", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/po/action/google?intent=erase", http.StatusFound)
	})
	Handler.mux.HandleFunc("/po/callback/google", handleGoogleCallback(googleConfig))
	Handler.mux.HandleFunc("POST /po/sign", handleSign)
	Handler.mux.HandleFunc("DELETE /po/shard", handleDeleteShard)
	Handler.mux.Handle("POST /po/register", http.HandlerFunc(handleRegister))
	Handler.mux.HandleFunc("/po/", pageHandler)
	L.Debug().Msg("operator service enabled")
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	global.Settings.Operator.Enabled = true
	if err := global.SaveUserSettings(); err != nil {
		L.Error().Err(err).Msg("failed to save settings")
		http.Error(w, "failed to save settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	SetupEnabled()
	http.Redirect(w, r, "/po/", http.StatusFound)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	global.Settings.Operator.Enabled = false
	if err := global.SaveUserSettings(); err != nil {
		L.Error().Err(err).Msg("failed to save settings")
		http.Error(w, "failed to save settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/po/", http.StatusFound)
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	operatorPage(loggedUser).Render(r.Context(), w)
}

func handleGoogleCallback(oauthConfig *oauth2.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, intent, err := common.HandleGoogleCallback(r, oauthConfig)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var reg Registration
		reg, err = loadRegistration(user.Email)
		if err != nil {
			if errors.Is(err, ErrAccountNotFound) {
				http.Error(w, "account not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to load registration", http.StatusInternalServerError)
			return
		}

		if intent == "erase" {
			http.SetCookie(w, &http.Cookie{
				Name:     "reallyDelete",
				Value:    user.Email,
				Path:     "/po",
				MaxAge:   300,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   r.TLS != nil,
			})
			confirmErasePage(user.Email).Render(r.Context(), w)
		} else {
			confirmRecoveryPage(user.Email, reg.Shard).Render(r.Context(), w)
		}
	}
}

func handleDeleteShard(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("reallyDelete")
	if err != nil {
		http.Error(w, "missing delete confirmation", http.StatusForbidden)
		return
	}

	email, err := mail.ParseAddress(cookie.Value)
	if err != nil || email.Address != cookie.Value {
		http.Error(w, "invalid delete confirmation", http.StatusForbidden)
		return
	}

	if err := deleteRegistration(cookie.Value); err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to delete registration", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "reallyDelete",
		Value:    "",
		Path:     "/po",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})

	w.WriteHeader(http.StatusNoContent)
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	var evt nostr.Event
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		http.Error(w, "failed to decode event", http.StatusBadRequest)
		return
	}
	if ok := evt.VerifySignature(); !ok {
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}
	if evt.Kind != common.KindOperatorRegistration {
		http.Error(w, "invalid kind", http.StatusBadRequest)
		return
	}

	// check registration filter
	switch global.Settings.Operator.RegistrationFilter {
	case "members":
		if !pyramid.IsMember(evt.PubKey) {
			http.Error(w, "only pyramid members can register", http.StatusForbidden)
			return
		}
	case "wot":
		if !wot.Computed {
			http.Error(w, "web-of-trust not yet computed, try again later", http.StatusServiceUnavailable)
			return
		}
		if !wot.Current.Contains(evt.PubKey) {
			http.Error(w, "only web-of-trust members can register", http.StatusForbidden)
			return
		}
	default:
		// allow anyone
	}

	emailTag := evt.Tags.Find("email")
	centralTag := evt.Tags.Find("central")

	if emailTag == nil || len(emailTag) < 2 || centralTag == nil || len(centralTag) < 2 {
		http.Error(w, "missing or invalid tags", http.StatusBadRequest)
		return
	}
	var shard frost.KeyShard
	if err := shard.DecodeHex(evt.Content); err != nil {
		http.Error(w, "invalid shard", http.StatusBadRequest)
		return
	}

	var pk nostr.PubKey
	shard.PublicKey.X.PutBytes((*[32]byte)(&pk))
	var our nostr.PubKey
	shard.PublicKeyShard.PublicKey.X.PutBytes((*[32]byte)(&our))

	req, err := http.NewRequestWithContext(
		r.Context(),
		http.MethodPost,
		fmt.Sprintf("%s/operator/ack", centralTag[1]),
		strings.NewReader(url.Values{
			"email": {emailTag[1]},
			"url":   {global.Settings.HTTPScheme() + global.Settings.Domain},
		}.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err != nil {
		http.Error(w, "failed to confirm with central: "+err.Error(), http.StatusBadGateway)
		return
	}
	req.Header.Set("X-Pomegranate-Operator-Token", r.Header.Get("X-Pomegranate-Operator-Token"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "failed to confirm with central: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, "failed to confirm with central: "+string(body), http.StatusBadGateway)
		return
	}

	centralInfo, err := nip11.Fetch(r.Context(), centralTag[1])
	if err != nil || centralInfo.Self == nil {
		http.Error(w, "failed to fetch central pubkey", http.StatusBadGateway)
		return
	}

	reg := Registration{
		Email:         emailTag[1],
		Central:       centralTag[1],
		CentralPubKey: centralInfo.Self.Hex(),
		Shard:         evt.Content,
	}

	if err := saveRegistration(reg); err != nil {
		http.Error(w, "failed to save registration", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}
