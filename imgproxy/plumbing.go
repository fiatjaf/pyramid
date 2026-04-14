package imgproxy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fiatjaf/pyramid/global"
)

func ensureImgproxyBinary() error {
	path := imgproxyBinaryPath()
	if ok, err := fileMatchesSHA256(path, imgproxySHA256); err == nil && ok {
		return nil
	}

	if err := os.MkdirAll(global.S.DataPath, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	temp, err := os.CreateTemp(global.S.DataPath, "imgproxy-bin-*")
	if err != nil {
		return fmt.Errorf("create imgproxy temp file: %w", err)
	}
	defer func() {
		temp.Close()
		os.Remove(temp.Name())
	}()

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Get(imgproxyURL)
	if err != nil {
		return fmt.Errorf("download imgproxy binary: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download imgproxy binary: github returned %s", resp.Status)
	}

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temp, hash), resp.Body); err != nil {
		return fmt.Errorf("download imgproxy binary: %w", err)
	}

	if hex.EncodeToString(hash.Sum(nil)) != imgproxySHA256 {
		return fmt.Errorf("download imgproxy binary: sha256 mismatch")
	}

	if err := temp.Close(); err != nil {
		return fmt.Errorf("close imgproxy temp file: %w", err)
	}
	if err := os.Chmod(temp.Name(), 0o755); err != nil {
		return fmt.Errorf("chmod imgproxy binary: %w", err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return fmt.Errorf("replace imgproxy binary: %w", err)
	}

	log.Info().Msg("downloaded imgproxy binary")
	return nil
}

func imgproxyBinaryPath() string {
	return filepath.Join(global.S.DataPath, "imgproxy-bin")
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

func ensureLibvips() error {
	if hasLibvips() {
		return nil
	}

	if !isUbuntu() {
		return fmt.Errorf("libvips not available")
	}

	if err := runCommand("apt-get", "update"); err != nil {
		return fmt.Errorf("apt-get update: %w", err)
	}
	if err := runCommand("apt-get", "install", "-y", "libvips"); err != nil {
		return fmt.Errorf("apt-get install libvips: %w", err)
	}
	if !hasLibvips() {
		return fmt.Errorf("libvips not available after install")
	}

	return nil
}

func hasLibvips() bool {
	return exec.Command("vips", "--version").Run() == nil
}

func isUbuntu() bool {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			id := strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
			return id == "ubuntu"
		}
	}
	return false
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(output))
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, msg)
}

func fileMatchesSHA256(path string, expected string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}
	return hex.EncodeToString(hash.Sum(nil)) == expected, nil
}
