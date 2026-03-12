package groups

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var LiveKitEmbedded bool

const embeddedLiveKitBinaryPath = "livekit-server"

var embeddedLiveKit = struct {
	mu      sync.RWMutex
	cmd     *exec.Cmd
	running bool
	error   string
	version string
	proxy   *httputil.ReverseProxy
}{
	proxy: &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&url.URL{Scheme: "http", Host: "127.0.0.1:7880"})
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "embedded livekit server is unavailable: "+err.Error(), http.StatusServiceUnavailable)
		},
	},
}

func stopEmbeddedLiveKitHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	if err := StopEmbeddedLiveKit(); err != nil {
		http.Error(w, "failed to stop embedded livekit: "+err.Error(), 500)
		return
	}

	http.Redirect(w, r, "/groups/", 302)
}

func startEmbeddedLiveKitHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}
	if !EmbeddedLiveKitAvailable() {
		http.Error(w, "embedded livekit requires pyramid to serve HTTPS on ports 443/80 with a configured domain", 400)
		return
	}

	if err := StartEmbeddedLiveKit(); err != nil {
		http.Error(w, "failed to start embedded livekit: "+err.Error(), 500)
		return
	}

	http.Redirect(w, r, "/groups/", 302)
}

type livekitRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func EmbeddedLiveKitRunning() bool {
	embeddedLiveKit.mu.RLock()
	defer embeddedLiveKit.mu.RUnlock()
	return embeddedLiveKit.running
}

func EmbeddedLiveKitError() string {
	embeddedLiveKit.mu.RLock()
	defer embeddedLiveKit.mu.RUnlock()
	return embeddedLiveKit.error
}

func EmbeddedLiveKitAvailable() bool {
	return global.S.Port == "443" && global.Settings.Domain != ""
}

func StartEmbeddedLiveKit() error {
	embeddedLiveKit.mu.Lock()
	defer embeddedLiveKit.mu.Unlock()

	if embeddedLiveKit.running {
		return nil
	}
	if !EmbeddedLiveKitAvailable() {
		err := fmt.Errorf("embedded livekit requires the relay to run on ports 443/80 with a configured domain")
		embeddedLiveKit.error = err.Error()
		return err
	}
	if global.Settings.Domain == "" {
		err := fmt.Errorf("set the relay domain before starting embedded livekit")
		embeddedLiveKit.error = err.Error()
		return err
	}

	release, err := fetchLatestLiveKitRelease()
	if err != nil {
		embeddedLiveKit.error = err.Error()
		return err
	}

	if err := ensureLiveKitBinary(release); err != nil {
		embeddedLiveKit.error = err.Error()
		return err
	}

	apiKey := "local" + randomToken(12)
	apiSecret := "local" + randomToken(24)

	config := `port: 7880
rtc:
  tcp_port: 7881
  use_external_ip: true
turn:
  enabled: true
  domain: turn.` + global.Settings.Domain + `
  udp_port: 3478
  tls_port: 5349
  external_tls: true
keys:
  '` + apiKey + `': '` + apiSecret + `'
`

	cmd := exec.Command("./"+embeddedLiveKitBinaryPath, "--config-body", config)
	logFile, err := os.OpenFile(filepath.Join(global.S.DataPath, "livekit.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		embeddedLiveKit.error = err.Error()
		return err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		embeddedLiveKit.error = err.Error()
		return err
	}

	exited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		logFile.Close()

		embeddedLiveKit.mu.Lock()
		defer embeddedLiveKit.mu.Unlock()

		if embeddedLiveKit.cmd == cmd {
			embeddedLiveKit.cmd = nil
			embeddedLiveKit.running = false
			if err != nil {
				embeddedLiveKit.error = err.Error()
				log.Error().Err(err).Msg("embedded livekit server exited")
			}
		}
		exited <- err
	}()

	select {
	case err := <-exited:
		if err == nil {
			err = fmt.Errorf("embedded livekit exited immediately")
		}
		embeddedLiveKit.error = err.Error()
		return err
	case <-time.After(1500 * time.Millisecond):
	}

	LiveKitEmbedded = true
	global.Settings.Groups.LiveKitServerURL = global.Settings.WSScheme() + "livekit." + global.Settings.Domain
	global.Settings.Groups.LiveKitAPIKey = apiKey
	global.Settings.Groups.LiveKitAPISecret = apiSecret
	if err := global.SaveUserSettings(); err != nil {
		_ = terminateProcess(cmd.Process)
		embeddedLiveKit.error = err.Error()
		return err
	}

	embeddedLiveKit.cmd = cmd
	embeddedLiveKit.running = true
	embeddedLiveKit.version = release.TagName
	embeddedLiveKit.error = ""
	log.Info().Str("version", release.TagName).Int("pid", cmd.Process.Pid).Msg("started embedded livekit server")
	return nil
}

func StopEmbeddedLiveKit() error {
	embeddedLiveKit.mu.Lock()
	defer embeddedLiveKit.mu.Unlock()

	if embeddedLiveKit.cmd != nil && embeddedLiveKit.cmd.Process != nil {
		if err := terminateProcess(embeddedLiveKit.cmd.Process); err != nil {
			embeddedLiveKit.error = err.Error()
			return err
		}
	}

	embeddedLiveKit.cmd = nil
	embeddedLiveKit.running = false

	LiveKitEmbedded = false
	global.Settings.Groups.LiveKitServerURL = ""
	global.Settings.Groups.LiveKitAPIKey = ""
	global.Settings.Groups.LiveKitAPISecret = ""
	if err := global.SaveUserSettings(); err != nil {
		embeddedLiveKit.error = err.Error()
		return err
	}

	embeddedLiveKit.error = ""
	log.Info().Msg("stopped embedded livekit server")
	return nil
}

func LiveKitProxyHandler(w http.ResponseWriter, r *http.Request) {
	if !EmbeddedLiveKitRunning() {
		http.Error(w, "embedded livekit server is not running", http.StatusServiceUnavailable)
		return
	}
	embeddedLiveKit.proxy.ServeHTTP(w, r)
}

func fetchLatestLiveKitRelease() (livekitRelease, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/livekit/livekit/releases/latest")
	if err != nil {
		return livekitRelease{}, fmt.Errorf("fetch latest livekit release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return livekitRelease{}, fmt.Errorf("fetch latest livekit release: github returned %s", resp.Status)
	}

	var release livekitRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return livekitRelease{}, fmt.Errorf("parse livekit release: %w", err)
	}

	return release, nil
}

func ensureLiveKitBinary(release livekitRelease) error {
	assetURL, shasum, err := livekitAssetForRuntime(release)
	if err != nil {
		return err
	}

	// inspect checksum to see if we have to download a new binary
	if file, err := os.Open(embeddedLiveKitBinaryPath); err == nil {
		defer file.Close()
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err == nil {
			if hex.EncodeToString(hash.Sum(nil)) == shasum {
				// we already have the latest version
				return nil
			}
		}
	}

	// download
	out, err := os.CreateTemp(".", "livekit-archive-*")
	if err != nil {
		return fmt.Errorf("create livekit archive: %w", err)
	}
	defer out.Close()

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Get(assetURL)
	if err != nil {
		return fmt.Errorf("download livekit binary: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download livekit binary: github returned %s", resp.Status)
	}

	// extract
	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("read livekit archive: %w", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read livekit archive entry: %w", err)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		if filepath.Base(hdr.Name) != "livekit-server" && filepath.Base(hdr.Name) != "livekit-server.exe" {
			continue
		}

		tempPath := embeddedLiveKitBinaryPath + ".tmp"
		file, err := os.Create(tempPath)
		if err != nil {
			return fmt.Errorf("create embedded livekit binary: %w", err)
		}
		if _, err := io.Copy(file, tr); err != nil {
			file.Close()
			os.Remove(tempPath)
			return fmt.Errorf("write embedded livekit binary: %w", err)
		}
		if err := file.Close(); err != nil {
			os.Remove(tempPath)
			return fmt.Errorf("close embedded livekit binary: %w", err)
		}
		if err := os.Chmod(tempPath, 0o755); err != nil {
			os.Remove(tempPath)
			return fmt.Errorf("chmod embedded livekit binary: %w", err)
		}
		if err := os.Rename(tempPath, embeddedLiveKitBinaryPath); err != nil {
			os.Remove(tempPath)
			return fmt.Errorf("replace embedded livekit binary: %w", err)
		}
		found = true
		break
	}
	if !found {
		return fmt.Errorf("embedded livekit binary not found in archive")
	}

	log.Info().Str("version", release.TagName).Msg("downloaded embedded livekit binary")
	return nil
}

func livekitAssetForRuntime(release livekitRelease) (string, string, error) {
	version := strings.TrimPrefix(release.TagName, "v")
	var suffix string
	switch runtime.GOARCH {
	case "amd64":
		suffix = "amd64"
	case "arm64":
		suffix = "arm64"
	case "arm":
		suffix = "armv7"
	default:
		return "", "", fmt.Errorf("unsupported livekit architecture %q", runtime.GOARCH)
	}

	assetName := fmt.Sprintf("livekit_%s_%s_%s.tar.gz", version, runtime.GOOS, suffix)
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return asset.URL, strings.TrimPrefix(asset.Digest, "sha256:"), nil
		}
	}

	return "", "", fmt.Errorf("no livekit release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
}

func terminateProcess(process *os.Process) error {
	if process == nil {
		return nil
	}

	if err := process.Signal(syscall.SIGTERM); err != nil && !strings.Contains(err.Error(), "process already finished") {
		return err
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := process.Signal(syscall.Signal(0)); err != nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	if err := process.Kill(); err != nil && !strings.Contains(err.Error(), "process already finished") {
		return err
	}
	return nil
}

func ShutdownEmbeddedLiveKit() {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = StopEmbeddedLiveKit()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		log.Warn().Msg("timed out stopping embedded livekit during shutdown")
	}
}
