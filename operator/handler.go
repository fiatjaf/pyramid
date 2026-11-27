package operator

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
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

	// requests are rate-limited per account: up to 32 counted at a time, with
	// one taken off the count every 18 seconds
	accountLimiter = common.NewRateLimiter(32, 18*time.Second)

	// ecdh has its own budget, of about the same size, so a lot of decrypting doesn't
	// keep the account from signing and vice-versa
	ecdhLimiter = common.NewRateLimiter(25, 20*time.Second)

	// nip-42 auth and blossom upload events tend to be signed in bursts, so
	// they get their own budget: three times bigger, draining 50% faster
	authLimiter = common.NewRateLimiter(96, 12*time.Second)
)

// allowRequest counts a request against the account, logging what it was for
// so we can tell what is eating an account's budget
func allowRequest(email string, what string) bool {
	return allow(accountLimiter, email, what)
}

// allowSign counts a signature against the account, using the bigger auth
// budget for the kinds that are signed in bursts
func allowSign(email string, kind nostr.Kind, what string) bool {
	if kind == nostr.KindClientAuthentication || kind == nostr.KindBlobs {
		return allow(authLimiter, email, what)
	}
	return allowRequest(email, what)
}

// allowECDH counts an ecdh against the account's separate ecdh budget
func allowECDH(email string) bool {
	return allow(ecdhLimiter, email, "ecdh")
}

func allow(limiter *common.RateLimiter, email string, what string) bool {
	email = common.NormalizeEmail(email)
	allowed, count := limiter.Allow(email)
	if allowed {
		L.Debug().Str("email", email).Str("what", what).Int("count", count).Msg("rate limit: counted")
	} else {
		L.Warn().Str("email", email).Str("what", what).Msg("rate limit: denied")
	}
	return allowed
}

func Init(relay *khatru.Relay) {
	L.Debug().Msg("initializing operator service")

	normalizeRegistrationKeys()

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
	Handler.mux.HandleFunc("/po/recover/google", handleRecoverStart("google"))
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
			renderErase(w, r, user.Email)
		} else {
			renderRecovery(w, r, user.Email, reg.Shard)
		}
	}
}

const (
	eraseCookieName  = "reallyDelete"
	originCookieName = "pomegranate_recover_origin"
	eraseTokenTTL    = 5 * time.Minute
)

// erase confirmations: after the oauth callback the browser only gets a random token,
// the email it stands for stays here, so a DELETE /po/shard can't be forged
type eraseConfirmation struct {
	email   string
	expires time.Time
}

var (
	eraseTokensMu sync.Mutex
	eraseTokens   = make(map[string]eraseConfirmation)
)

func newEraseToken(email string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)

	eraseTokensMu.Lock()
	defer eraseTokensMu.Unlock()
	now := time.Now()
	for t, c := range eraseTokens {
		if now.After(c.expires) {
			delete(eraseTokens, t)
		}
	}
	eraseTokens[token] = eraseConfirmation{email: email, expires: now.Add(eraseTokenTTL)}
	return token, nil
}

// takeEraseToken returns the email a token was issued for, and burns it
func takeEraseToken(token string) (string, bool) {
	eraseTokensMu.Lock()
	defer eraseTokensMu.Unlock()
	c, ok := eraseTokens[token]
	delete(eraseTokens, token)
	if !ok || time.Now().After(c.expires) {
		return "", false
	}
	return c.email, true
}

func renderErase(w http.ResponseWriter, r *http.Request, email string) {
	token, err := newEraseToken(email)
	if err != nil {
		http.Error(w, "failed to generate delete confirmation", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     eraseCookieName,
		Value:    token,
		Path:     "/po",
		MaxAge:   int(eraseTokenTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	confirmErasePage(email).Render(r.Context(), w)
}

// handleRecoverStart remembers the origin of the page that opened the recovery popup
// (given explicitly or taken from the referer) so the shard is only ever posted back
// to it, and so the user can see who is asking before handing it over
func handleRecoverStart(provider string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requested := r.URL.Query().Get("origin")
		if requested == "" {
			requested = r.Referer()
		}
		origin, ok := parseOrigin(requested)
		if !ok {
			http.Error(w, "missing or invalid origin of the page asking for the shard", http.StatusBadRequest)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     originCookieName,
			Value:    origin,
			Path:     "/po",
			MaxAge:   600,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   r.TLS != nil,
		})
		http.Redirect(w, r, "/po/action/"+provider+"?intent=recover", http.StatusFound)
	}
}

func parseOrigin(s string) (string, bool) {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", false
	}
	return u.Scheme + "://" + u.Host, true
}

func renderRecovery(w http.ResponseWriter, r *http.Request, email string, shard string) {
	cookie, err := r.Cookie(originCookieName)
	if err != nil {
		http.Error(w, "missing origin of the page asking for the shard, start the recovery again", http.StatusBadRequest)
		return
	}
	origin, ok := parseOrigin(cookie.Value)
	if !ok {
		http.Error(w, "invalid origin of the page asking for the shard", http.StatusBadRequest)
		return
	}
	confirmRecoveryPage(email, shard, origin).Render(r.Context(), w)
}

func handleDeleteShard(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(eraseCookieName)
	if err != nil {
		http.Error(w, "missing delete confirmation", http.StatusForbidden)
		return
	}

	email, ok := takeEraseToken(cookie.Value)
	if !ok {
		http.Error(w, "invalid or expired delete confirmation", http.StatusForbidden)
		return
	}

	if err := deleteRegistration(email); err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to delete registration", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     eraseCookieName,
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
		if !wot.IsComputed() {
			http.Error(w, "web-of-trust not yet computed, try again later", http.StatusServiceUnavailable)
			return
		}
		if !wot.Contains(evt.PubKey) {
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

	if !allowRequest(emailTag[1], "register") {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	var shard frost.KeyShard
	if err := shard.DecodeHex(evt.Content); err != nil {
		http.Error(w, "invalid shard", http.StatusBadRequest)
		return
	}

	var pk nostr.PubKey
	shard.PublicKey.X.PutBytes((*[32]byte)(&pk))
	if pk != evt.PubKey {
		L.Warn().Str("email", emailTag[1]).Str("pubkey", evt.PubKey.Hex()).Str("shard", pk.Hex()).Msg("op-register: shard pubkey mismatch")
		http.Error(w, "shard does not belong to the pubkey that signed the registration", http.StatusBadRequest)
		return
	}

	existing, err := loadRegistration(emailTag[1])
	if err != nil && !errors.Is(err, ErrAccountNotFound) {
		L.Error().Err(err).Str("email", emailTag[1]).Msg("op-register: load existing registration")
		http.Error(w, "failed to load registration", http.StatusInternalServerError)
		return
	}
	if err == nil {
		// if a registration already exists for this email, require it to be from the same public key
		// (this prevents rogue actors from overwriting other people's shard registrations)
		// if one wants to use a new keypair with their previous email they'll have to delete their shards manually first
		if existing.PubKey == "" {
			// DEPRECATED, remove these hardcoded urls in 2028, start enforcing only full pubkey match (or maybe trust some preconfigured centrals?)
			if centralTag[1] != "https://auth.njump.me" && centralTag[1] != "https://auth.yakihonne.com" {
				L.Warn().Str("email", emailTag[1]).Str("central", centralTag[1]).Msg("op-register: legacy empty pubkey registration rejected by central")
				http.Error(w, "a pubkey is already registered for this email, but it was never stored; this central cannot claim it", http.StatusForbidden)
				return
			}
		} else if existing.PubKey != evt.PubKey.Hex() {
			L.Warn().Str("email", emailTag[1]).Str("old", existing.PubKey).Str("new", evt.PubKey.Hex()).Msg("op-register: pubkey mismatch")
			http.Error(w, "a different pubkey is already registered for this email", http.StatusForbidden)
			return
		}
	}

	// confirming with central is the last step, since after that central
	// considers us registered: everything that can make us refuse must come first
	centralInfo, err := nip11.Fetch(r.Context(), centralTag[1])
	if err != nil || centralInfo.Self == nil {
		http.Error(w, "failed to fetch central pubkey", http.StatusBadGateway)
		return
	}

	req, err := http.NewRequestWithContext(
		r.Context(),
		http.MethodPost,
		fmt.Sprintf("%s/operator/ack", centralTag[1]),
		strings.NewReader(url.Values{
			"email": {common.NormalizeEmail(emailTag[1])},
			"url":   {global.Settings.HTTPScheme() + global.Settings.Domain},
		}.Encode()),
	)
	if err != nil {
		http.Error(w, "failed to confirm with central: "+err.Error(), http.StatusBadGateway)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

	reg := Registration{
		Email:         emailTag[1],
		PubKey:        evt.PubKey.Hex(),
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
