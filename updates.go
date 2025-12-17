package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/fiatjaf/pyramid/global"
)

// this is set at build time to something else based on git
var currentVersion string = "dev"

type releaseVersion struct {
	name      string
	binaryURL string
}

var latestVersion releaseVersion

func fetchLatestVersion() {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/fiatjaf/pyramid/releases/latest")
	if err != nil {
		log.Error().Err(err).Msg("failed to fetch latest release from github")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("status", resp.StatusCode).Msg("github api returned non-200 status")
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().Err(err).Msg("failed to read github api response")
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		log.Error().Err(err).Msg("failed to parse github api response")
		return
	}

	// determine architecture and find the corresponding binary asset
	var binaryURL string
	var expectedBinaryName string
	switch runtime.GOARCH {
	case "amd64":
		expectedBinaryName = "pyramid-amd64"
	case "arm64":
		expectedBinaryName = "pyramid-arm64"
	default:
		log.Error().Str("arch", runtime.GOARCH).Msg("unsupported architecture")
		return
	}

	for _, asset := range release.Assets {
		if asset.Name == expectedBinaryName {
			binaryURL = asset.URL
			break
		}
	}
	if binaryURL == "" {
		log.Error().Str("binary", expectedBinaryName).Msg("binary asset not found in latest release")
		return
	}

	latestVersion = releaseVersion{
		name:      release.TagName,
		binaryURL: binaryURL,
	}
	log.Info().Str("version", latestVersion.name).Msg("fetched latest version from github")
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
	cancelStartContext(updating)
	global.End()
	err = syscall.Exec(absPath, append([]string{absPath}, os.Args[1:]...), os.Environ())

	// if we reach here, exec failed
	return fmt.Errorf("exec failed: %w", err)
}
