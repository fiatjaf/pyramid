package corsproxy

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/rs/cors"
)

var (
	log     = global.Log.With().Str("service", "corsproxy").Logger()
	Handler = &MuxHandler{}

	client = &http.Client{Timeout: 30 * time.Second}

	baseSecret []byte
)

func Init() {
	baseSecret, _ = hex.DecodeString(global.Settings.CorsProxy.BaseSecret)
	Setup()
}

// Setup (re)builds the mux based on whether the proxy is enabled.
func Setup() {
	baseSecret, _ = hex.DecodeString(global.Settings.CorsProxy.BaseSecret)
	Handler.mux = http.NewServeMux()
	if global.Settings.CorsProxy.Enabled {
		Handler.mux.HandleFunc("POST /corsproxy/disable", disableHandler)
		Handler.mux.HandleFunc("POST /corsproxy/prepare", prepareHandler)
		Handler.mux.Handle("/corsproxy/secret", cors.AllowAll().Handler(http.HandlerFunc(secretHandler)))
	}
	Handler.mux.HandleFunc("/corsproxy/", pageHandler)
	Handler.mux.HandleFunc("/corsproxy", pageHandler)
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/corsproxy/" || r.URL.Path == "/corsproxy" {
		loggedUser, _ := global.GetLoggedUser(r)
		corsProxyPage(loggedUser).Render(r.Context(), w)
		return
	}

	if !global.Settings.CorsProxy.Enabled {
		http.NotFound(w, r)
		return
	}

	spl := strings.Split(r.URL.Path, "/")
	// expected shapes:
	//   /corsproxy/{token}/{b64-source-url}      -> per-user token auth
	//   /corsproxy/{b64-source-url}              -> origin-filter mode (no token)
	if len(spl) < 3 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	var sourceURLB64 string
	if rawToken, err := base64.RawURLEncoding.DecodeString(spl[2]); err == nil && len(rawToken) == 48 && len(spl) >= 4 {
		// token mode
		sourceURLB64 = spl[3]
		if err := verifyToken(rawToken, sourceURLB64); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
	} else {
		// origin-filter mode
		if !global.OriginAllowed(r, global.Settings.CorsProxy.AllowedDomains) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sourceURLB64 = spl[2]
	}

	sourceURLBytes, err := base64.RawURLEncoding.DecodeString(sourceURLB64)
	if err != nil {
		http.Error(w, "invalid source url encoding", http.StatusBadRequest)
		return
	}
	sourceURL := string(sourceURLBytes)
	if !strings.HasPrefix(sourceURL, "http://") && !strings.HasPrefix(sourceURL, "https://") {
		http.Error(w, "source url must be http(s)", http.StatusBadRequest)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, sourceURL, r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// copy through a safe subset of headers
	for _, h := range []string{"Accept", "Accept-Encoding", "Accept-Language", "Content-Type"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	if v := r.Header.Get("Range"); v != "" {
		req.Header.Set("Range", v)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Warn().Err(err).Str("url", sourceURL).Msg("cors proxy fetch failed")
		http.Error(w, "failed to fetch upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// copy headers back, plus permissive CORS headers so browsers allow the cross-origin read
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Expose-Headers", "*")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	for k, vs := range resp.Header {
		if strings.EqualFold(k, "Access-Control-Allow-Origin") ||
			strings.EqualFold(k, "Access-Control-Allow-Methods") ||
			strings.EqualFold(k, "Access-Control-Allow-Headers") {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

//go:inline
func pubkeySecretFor(pubkey []byte) []byte {
	b := hmac.New(sha256.New, baseSecret)
	b.Write(pubkey)
	return b.Sum(nil)
}

func verifyToken(token []byte, sourceURLB64 string) error {
	mac := token[0:16]
	pubkey := token[16 : 16+32]

	pubkeySecret := pubkeySecretFor(pubkey)

	sourceURLBytes, err := base64.RawURLEncoding.DecodeString(sourceURLB64)
	if err != nil {
		return fmt.Errorf("invalid source url encoding")
	}

	h := hmac.New(sha256.New, pubkeySecret)
	h.Write(sourceURLBytes)
	expected := h.Sum(nil)
	if !bytes.Equal(expected[0:16], mac) {
		return fmt.Errorf("mac doesn't match")
	}
	return nil
}

func prepareURLPath(pubkey nostr.PubKey, sourceURL string) string {
	macPath := sourceURL

	pubkeySecret := pubkeySecretFor(pubkey[:])

	h := hmac.New(sha256.New, pubkeySecret)
	h.Write([]byte(macPath))
	mac := h.Sum(nil)

	token := make([]byte, 48)
	copy(token[0:16], mac)
	copy(token[16:48], pubkey[:])

	encoded := base64.RawURLEncoding.EncodeToString([]byte(sourceURL))
	full := "/" + base64.RawURLEncoding.EncodeToString(token) + "/" + encoded
	return full
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	global.Settings.CorsProxy.Enabled = false
	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}
	Setup()
	http.Redirect(w, r, "/corsproxy/", http.StatusSeeOther)
}

func secretHandler(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Nostr ") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	evtj, err := base64.StdEncoding.DecodeString(auth[6:])
	if err != nil {
		http.Error(w, "invalid base64-encoded event", http.StatusUnauthorized)
		return
	}
	var evt nostr.Event
	if err := json.Unmarshal(evtj, &evt); err != nil {
		http.Error(w, "invalid event", http.StatusUnauthorized)
		return
	}
	if evt.Kind != 27235 {
		http.Error(w, "invalid kind", http.StatusUnauthorized)
		return
	}
	if !evt.VerifySignature() {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	if tag := evt.Tags.Find("u"); tag == nil || tag[1] != global.Settings.HTTPScheme()+global.Settings.Domain+r.URL.Path {
		http.Error(w, "invalid url", http.StatusUnauthorized)
		return
	}

	secret := pubkeySecretFor(evt.PubKey[:])
	w.Write([]byte(hex.EncodeToString(secret)))
}

func prepareHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsMember(loggedUser) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sourceURL := r.PostFormValue("url")
	if sourceURL == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "url is required"})
		return
	}

	path := prepareURLPath(loggedUser, sourceURL)
	fullURL := global.Settings.HTTPScheme() + global.Settings.Domain + "/corsproxy" + path

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": fullURL})
}
