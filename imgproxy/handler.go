package imgproxy

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/rs/cors"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	imgproxyURL    = "https://github.com/fiatjaf/pyramid/releases/download/v1.2.3/imgproxy-bin"
	imgproxySHA256 = "a97e132c46f49ba70541c3de86e2664cdf8744f9274f4240380a90202912f948"
)

var (
	log     = global.Log.With().Str("service", "imgproxy").Logger()
	Handler = &MuxHandler{}

	imgproxySocketClient *http.Client
	baseSecret           []byte
)

var state = struct {
	mu       sync.RWMutex
	cmd      *exec.Cmd
	running  bool
	starting bool
	err      string
}{}

func Init() {
	// decode the stored base secret now that settings are loaded (package-level
	// initializers run before global.Init() populates the settings)
	baseSecret, _ = hex.DecodeString(global.Settings.Imgproxy.BaseSecret)

	if !global.Settings.Imgproxy.Enabled {
		setupDisabled()
		return
	}

	setupEnabled()
	go startImgproxy()
}

func setupDisabled() {
	imgproxySocketClient = nil
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /imgproxy/enable", enableHandler)
	Handler.mux.HandleFunc("GET /imgproxy/log", logHandler)
	Handler.mux.HandleFunc("/imgproxy/", pageHandler)
}

func setupEnabled() {
	socketFile, _ := filepath.Abs(filepath.Join(global.S.DataPath, "imgproxy.sock"))

	imgproxySocketClient = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketFile)
		},
	}}

	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /imgproxy/disable", disableHandler)
	Handler.mux.HandleFunc("POST /imgproxy/prepare", prepareHandler)
	Handler.mux.HandleFunc("GET /imgproxy/log", logHandler)
	Handler.mux.Handle("/imgproxy/secret", cors.AllowAll().Handler(http.HandlerFunc(secretHandler)))
	Handler.mux.HandleFunc("/imgproxy/", pageHandler)
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/imgproxy/" && r.URL.Path != "/imgproxy" {
		if !global.Settings.Imgproxy.Enabled {
			http.NotFound(w, r)
			return
		}

		spl := strings.Split(r.URL.Path, "/")

		// decode special token from url
		token, err := base64.RawURLEncoding.DecodeString(spl[2])

		imgproxyURL := ""
		if err == nil && len(token) == 48 {
			// token mode: validate mac against the path that follows
			path := "/" + strings.Join(spl[3:], "/")
			decodedPath, _ := url.PathUnescape(path)

			mac := token[0:16]
			pubkey := token[16 : 16+32]

			pubkeySecret := pubkeySecretFor(pubkey)

			h := hmac.New(sha256.New, pubkeySecret)
			h.Write([]byte(decodedPath))
			expected := h.Sum(nil)
			if !hmac.Equal(expected[0:16], mac) {
				http.Error(w, "mac doesn't match", http.StatusUnauthorized)
				return
			}

			imgproxyURL = "http://imgproxy/insecure" + path
		}

		resp, err := imgproxySocketClient.Get(imgproxyURL)
		if err != nil {
			log.Error().Err(err).Msg("imgproxy request failed")
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, v := range resp.Header {
			for _, hv := range v {
				w.Header().Add(k, hv)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	loggedUser, _ := global.GetLoggedUser(r)

	imgproxyPage(loggedUser, state.running, state.err).Render(r.Context(), w)
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	state.err = ""
	global.Settings.Imgproxy.Enabled = true
	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	if global.Settings.Imgproxy.BaseSecret == "" {
		baseSecret = make([]byte, 16)
		rand.Read(baseSecret)
		global.Settings.Imgproxy.BaseSecret = hex.EncodeToString(baseSecret)
	}

	setupEnabled()
	go startImgproxy()
	http.Redirect(w, r, "/imgproxy/", http.StatusSeeOther)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	global.Settings.Imgproxy.Enabled = false
	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	stopImgproxy()
	state.err = ""
	setupDisabled()
	http.Redirect(w, r, "/imgproxy/", http.StatusSeeOther)
}

func startImgproxy() {
	state.mu.Lock()
	if state.running || state.starting {
		state.mu.Unlock()
		return
	}
	state.starting = true
	state.mu.Unlock()

	defer func() {
		state.mu.Lock()
		state.starting = false
		state.mu.Unlock()
	}()

	if err := ensureImgproxyBinary(); err != nil {
		disableImgproxyWithError(err)
		return
	}

	if err := ensureLibvips(); err != nil {
		disableImgproxyWithError(err)
		return
	}

	rotator := &lumberjack.Logger{
		Filename:   filepath.Join(global.S.DataPath, "imgproxy.log"),
		MaxSize:    10,
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	cmd := exec.Command(imgproxyBinaryPath())
	cmd.Stdout = rotator
	cmd.Stderr = rotator

	socketFile, _ := filepath.Abs(filepath.Join(global.S.DataPath, "imgproxy.sock"))
	env := append(os.Environ(),
		"IMGPROXY_BIND="+socketFile,
		"IMGPROXY_NETWORK=unix",
		"IMGPROXY_TIMEOUT=5",
		"IMGPROXY_USER_AGENT=imgproxy/pyramid",
		"IMGPROXY_USE_ETAG=true",
	)
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		disableImgproxyWithError(err)
		return
	}

	state.mu.Lock()
	state.cmd = cmd
	state.running = true
	state.err = ""
	state.mu.Unlock()

	go func(cmd *exec.Cmd) {
		err := cmd.Wait()
		if err == nil {
			err = fmt.Errorf("imgproxy exited")
		}

		state.mu.Lock()
		if state.cmd == cmd {
			state.cmd = nil
			state.running = false
			if err != nil {
				state.err = err.Error()
			}
		}
		state.mu.Unlock()

		if err != nil && global.Settings.Imgproxy.Enabled {
			disableImgproxyWithError(err)
		}
	}(cmd)
}

func stopImgproxy() {
	state.mu.Lock()
	cmd := state.cmd
	state.cmd = nil
	state.running = false
	state.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	if err := terminateProcess(cmd.Process); err != nil {
		log.Warn().Err(err).Msg("failed to stop imgproxy")
	}
}

func disableImgproxyWithError(err error) {
	if err == nil {
		return
	}

	state.err = err.Error()
	global.Settings.Imgproxy.Enabled = false
	if saveErr := global.SaveUserSettings(); saveErr != nil {
		log.Error().Err(saveErr).Msg("failed to save imgproxy disabled state")
	}
	stopImgproxy()
	setupDisabled()
}

//go:inline
func pubkeySecretFor(pubkey []byte) []byte {
	b := hmac.New(sha256.New, baseSecret)
	b.Write(pubkey)
	return b.Sum(nil)
}

func prepareURLPath(pubkey nostr.PubKey, options, sourceURL string) string {
	decodedURL, _ := url.PathUnescape(sourceURL)
	encoded := strings.NewReplacer(
		"%", "%25",
		"/", "%2F",
		"?", "%3F",
		"@", "%40",
	).Replace(decodedURL)

	imgproxyPath := "/" + options + "/plain/" + encoded + "@avif"
	macPath := "/" + options + "/plain/" + decodedURL + "@avif"

	pubkeySecret := pubkeySecretFor(pubkey[:])

	h := hmac.New(sha256.New, pubkeySecret)
	h.Write([]byte(macPath))
	mac := h.Sum(nil)

	token := make([]byte, 48)
	copy(token[0:16], mac)
	copy(token[16:48], pubkey[:])

	full := "/" + base64.RawURLEncoding.EncodeToString(token) + imgproxyPath
	return full
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
	options := r.PostFormValue("options")
	if sourceURL == "" || options == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "url and options are required"})
		return
	}

	path := prepareURLPath(loggedUser, options, sourceURL)
	fullURL := global.Settings.HTTPScheme() + global.Settings.Domain + "/imgproxy" + path

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": fullURL})
}

func logHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, ok := global.GetLoggedUser(r)
	if !ok || !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.LogHandler(w, r, filepath.Join(global.S.DataPath, "imgproxy.log"))
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}
