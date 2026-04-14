package imgproxy

import (
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
)

const (
	imgproxyURL    = "https://github.com/fiatjaf/pyramid/releases/download/v1.2.3/imgproxy-bin"
	imgproxySHA256 = "a97e132c46f49ba70541c3de86e2664cdf8744f9274f4240380a90202912f948"
)

var (
	log     = global.Log.With().Str("service", "imgproxy").Logger()
	Handler = &MuxHandler{}
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
	imgproxyReverseProxy = nil
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /imgproxy/enable", enableHandler)
	Handler.mux.HandleFunc("POST /imgproxy/keys", saveKeysHandler)
	Handler.mux.HandleFunc("/imgproxy/", pageHandler)
}

func setupEnabled() {
	target := &url.URL{Scheme: "http", Host: "localhost:8040"}
	imgproxyReverseProxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, "/imgproxy")
		},
	}
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /imgproxy/disable", disableHandler)
	Handler.mux.HandleFunc("POST /imgproxy/keys", saveKeysHandler)
	Handler.mux.HandleFunc("/imgproxy/", pageHandler)
}

var imgproxyReverseProxy *httputil.ReverseProxy

func pageHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/imgproxy/" && r.URL.Path != "/imgproxy" {
		if !global.Settings.Imgproxy.Enabled {
			http.NotFound(w, r)
			return
		}
		imgproxyReverseProxy.ServeHTTP(w, r)
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

	logFile, err := os.OpenFile(filepath.Join(global.S.DataPath, "imgproxy.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		disableImgproxyWithError(err)
		return
	}

	cmd := exec.Command(imgproxyBinaryPath())
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	env := append(os.Environ(), "IMGPROXY_BIND=:8040")
	if global.Settings.Imgproxy.Key != "" && global.Settings.Imgproxy.Salt != "" {
		env = append(env,
			"IMGPROXY_KEY="+global.Settings.Imgproxy.Key,
			"IMGPROXY_SALT="+global.Settings.Imgproxy.Salt,
		)
	}
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		logFile.Close()
		disableImgproxyWithError(err)
		return
	}

	state.mu.Lock()
	state.cmd = cmd
	state.running = true
	state.err = ""
	state.mu.Unlock()

	go func(cmd *exec.Cmd, logFile *os.File) {
		err := cmd.Wait()
		logFile.Close()
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
	}(cmd, logFile)
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

func saveKeysHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	global.Settings.Imgproxy.Key = strings.TrimSpace(r.PostFormValue("key"))
	global.Settings.Imgproxy.Salt = strings.TrimSpace(r.PostFormValue("salt"))
	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save keys", http.StatusInternalServerError)
		return
	}

	stopImgproxy()
	go startImgproxy()
	http.Redirect(w, r, "/imgproxy/", http.StatusSeeOther)
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}
