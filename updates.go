package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/groups"
	"github.com/fiatjaf/pyramid/search"
)

// this is set at build time to something else based on git
var currentVersion string = "dev"

// this is set by the user and reset on restart
var customUpdateSource string

type releaseVersion struct {
	name      string
	binaryURL string
}

var latestVersion releaseVersion

func fetchLatestVersion() {
	var (
		version releaseVersion
		err     error
	)
	if customUpdateSource == "" {
		version, err = fetchLatestFromGitHub("fiatjaf/pyramid")
	} else {
		version, err = resolveCustomUpdateSource(customUpdateSource)
	}

	if err != nil {
		log.Error().Err(err).Str("source", customUpdateSource).Msg("failed to fetch latest release")
		return
	}

	latestVersion = version
	log.Info().Str("version", latestVersion.name).Str("source", customUpdateSource).Msg("fetched latest version")
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func fetchLatestFromGitHub(repo string) (releaseVersion, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	release, err := fetchGitHubRelease(apiURL)
	if err != nil {
		return releaseVersion{}, err
	}
	return releaseToVersion(release)
}

func fetchReleaseByTag(repo, tag string) (releaseVersion, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, tag)
	release, err := fetchGitHubRelease(apiURL)
	if err != nil {
		return releaseVersion{}, err
	}
	return releaseToVersion(release)
}

func fetchGitHubRelease(apiURL string) (githubRelease, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return githubRelease{}, fmt.Errorf("failed to fetch github release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return githubRelease{}, fmt.Errorf("failed to read github api response: %w", err)
	}

	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return githubRelease{}, fmt.Errorf("failed to parse github api response: %w", err)
	}

	return release, nil
}

func releaseToVersion(release githubRelease) (releaseVersion, error) {
	// determine architecture and find the corresponding binary asset
	var binaryURL string
	var expectedBinaryName string
	switch runtime.GOARCH {
	case "amd64":
		expectedBinaryName = "pyramid-amd64"
	case "arm64":
		expectedBinaryName = "pyramid-arm64"
	default:
		return releaseVersion{}, fmt.Errorf("unsupported architecture %s", runtime.GOARCH)
	}

	for _, asset := range release.Assets {
		if asset.Name == expectedBinaryName {
			binaryURL = asset.URL
			break
		}
	}
	if binaryURL == "" {
		return releaseVersion{}, fmt.Errorf("binary asset not found for %s", expectedBinaryName)
	}

	return releaseVersion{
		name:      release.TagName,
		binaryURL: binaryURL,
	}, nil
}

func resolveCustomUpdateSource(source string) (releaseVersion, error) {
	u, err := url.Parse(source)
	if err != nil {
		return releaseVersion{}, fmt.Errorf("invalid update source url: %w", err)
	}
	host := strings.ToLower(u.Host)
	switch host {
	case "github.com", "www.github.com":
		return resolveGitHubWebURL(u, source)
	case "api.github.com":
		release, err := fetchGitHubRelease(u.String())
		if err != nil {
			return releaseVersion{}, err
		}
		return releaseToVersion(release)
	default:
		name := path.Base(u.Path)
		if name == "." || name == "/" || name == "" {
			name = "custom"
		}
		return releaseVersion{name: name, binaryURL: source}, nil
	}
}

func resolveGitHubWebURL(u *url.URL, raw string) (releaseVersion, error) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return releaseVersion{}, fmt.Errorf("invalid github repository url")
	}
	repo := parts[0] + "/" + parts[1]
	if len(parts) == 2 {
		return fetchLatestFromGitHub(repo)
	}
	if len(parts) >= 3 && parts[2] == "releases" {
		if len(parts) == 3 || parts[3] == "" || parts[3] == "latest" {
			return fetchLatestFromGitHub(repo)
		}
		switch parts[3] {
		case "tag":
			if len(parts) >= 5 {
				return fetchReleaseByTag(repo, parts[4])
			}
		case "download":
			if len(parts) >= 5 {
				return releaseVersion{name: parts[4], binaryURL: raw}, nil
			}
		}
	}
	return fetchLatestFromGitHub(repo)
}

func performUpdateInPlace() error {
	log.Info().Str("version", latestVersion.name).Msg("performing in-place update")
	if latestVersion.binaryURL == "" {
		return fmt.Errorf("no update available")
	}

	currentBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// download the new binary
	log.Info().Str("url", latestVersion.binaryURL).Msg("downloading version for update")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(latestVersion.binaryURL)
	if err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		body := string(b)
		if len(body) > 200 {
			body = body[0:199] + "…"
		}
		log.Warn().Str("body", body).Int("status", resp.StatusCode).Msg("github failed to serve us the binary again")
		return fmt.Errorf("downloading the new binary from github failed with status %d", resp.StatusCode)
	}
	// save the new binary to a stable path (overwrite it if it exists)
	tempPath := fmt.Sprintf("pyramid-update-%s", latestVersion.name)
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempPath)
	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to write binary: %w", err)
	}
	tempFile.Close()

	// use rename for atomic replacement
	log.Info().Msg("replacing binary with new version")
	if err := os.Rename(currentBinary, "pyramid-old-binary"); err != nil {
		return fmt.Errorf("replace failed: %w", err)
	}
	if err := os.Rename(tempPath, currentBinary); err != nil {
		return fmt.Errorf("replace failed: %w", err)
	}
	// ensure executable permissions on the final binary
	if err := os.Chmod(currentBinary, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	log.Info().Msg("restarting process with new binary...")
	// get the absolute path (syscall.Exec requires absolute path)
	absPath, err := filepath.Abs(currentBinary)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// execute the new binary, replacing current process
	// this call does not return if successful, therefore we must perform a graceful deinitialization of all things
	groups.ShutdownEmbeddedLiveKit()
	cancelStartContext(updating)
	global.End()
	search.End()
	err = syscall.Exec(absPath, append([]string{absPath}, os.Args[1:]...), os.Environ())

	// if we reach here, exec failed
	return fmt.Errorf("exec failed: %w", err)
}
