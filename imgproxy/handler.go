package imgproxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	imgproxyURL    = "https://github.com/fiatjaf/pyramid/releases/download/v1.2.3/imgproxy-bin"
	imgproxySHA256 = "a97e132c46f49ba70541c3de86e2664cdf8744f9274f4240380a90202912f948"
)

var (
	log     = global.Log.With().Str("service", "imgproxy").Logger()
	Handler = &MuxHandler{}

	reverseProxy *httputil.ReverseProxy
)

var state = struct {
	mu       sync.RWMutex
	cmd      *exec.Cmd
	running  bool
	starting bool
	err      string
}{}

func Init() {
	if !global.Settings.Imgproxy.Enabled {
		setupDisabled()
		return
	}

	setupEnabled()
	go startImgproxy()
}

func setupDisabled() {
	reverseProxy = nil
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /imgproxy/enable", enableHandler)
	Handler.mux.HandleFunc("GET /imgproxy/log", logHandler)
	Handler.mux.HandleFunc("/imgproxy/", pageHandler)
}

func setupEnabled() {
	target := &url.URL{Scheme: "http", Host: "localhost:38040"}
	reverseProxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			fmt.Println("in:", r.In.URL)

			r.SetURL(target)
			r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, "/imgproxy")
		},
	}
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /imgproxy/disable", disableHandler)
	Handler.mux.HandleFunc("POST /imgproxy/prepare", prepareHandler)
	Handler.mux.HandleFunc("GET /imgproxy/log", logHandler)
	Handler.mux.HandleFunc("/imgproxy/", pageHandler)
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/imgproxy/" && r.URL.Path != "/imgproxy" {
		if !global.Settings.Imgproxy.Enabled {
			http.NotFound(w, r)
			return
		}
		reverseProxy.ServeHTTP(w, r)
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
	env := append(os.Environ(),
		"IMGPROXY_BIND=:38040",
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

func prepareURL(options, sourceURL string) string {
	// path := "/" + options + "/" + base64.RawURLEncoding.EncodeToString([]byte(sourceURL)) + ".png"
	// if global.Settings.Imgproxy.Key == "" {
	// 	return "/insecure/" + path
	// }

	// key, err := hex.DecodeString(global.Settings.Imgproxy.Key)
	// if err != nil {
	// 	return ""
	// }
	// mac := hmac.New(sha256.New, key)
	// mac.Write([]byte(global.Settings.Imgproxy.Salt))
	// mac.Write([]byte(path))
	// signature := hex.EncodeToString(mac.Sum(nil))

	// return "/" + signature + path
	return ""
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

	path := prepareURL(options, sourceURL)
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
