package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

// The full-backup endpoint is gated by a random token stored next to the data
// (mode
// 0600): only something that can already read the relay's filesystem — the
// operator — can call it. This deliberately avoids loopback-address checks
// (local proxies like a tor sidecar would defeat those).
func exportTokenPath() string {
	return filepath.Join(global.S.DataPath, ".export-token")
}

func ensureExportToken() (string, error) {
	path := exportTokenPath()
	if data, err := os.ReadFile(path); err == nil && len(data) >= 32 {
		return string(data), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(token), 0600); err != nil {
		return "", err
	}
	return token, nil
}

// exportAuthorized accepts either the operator token (X-Export-Token, used by
// the platform backup pipeline and CLI) or a logged-in root user (the nip98
// cookie, used by the admin UI download button). Either one proves control of
// the relay: the token file is only readable by the operator, and root status
// is the relay's own authorization model.
func exportAuthorized(r *http.Request) bool {
	if got := r.Header.Get("X-Export-Token"); got != "" {
		if expected, err := ensureExportToken(); err == nil &&
			subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1 {
			return true
		}
	}
	if pk, ok := global.GetLoggedUser(r); ok && pyramid.IsRoot(pk) {
		return true
	}
	return false
}

// backupLayer names an mmm layer for the full-archive export.
//
// Two layers are omitted on purpose:
//   - "groups": its events are migrated into main at startup (see global.Init),
//     so main already contains all NIP-29 group data.
//   - "system": the sdk cache (profiles, relay lists fetched from the network).
//     It is re-derivable, holds no authoritative relay data, and dwarfs
//     everything else (200MB+ vs a ~17MB main on a real relay), so backing it
//     up would bloat the archive ~10x for regenerable cache.
type backupLayer struct {
	name string
	il   *mmm.IndexingLayer
}

func backupLayers() []backupLayer {
	return []backupLayer{
		{"main", global.IL.Main},
		{"internal", global.IL.Internal},
		{"personal", global.IL.Personal},
		{"favorites", global.IL.Favorites},
		{"inbox", global.IL.Inbox},
		{"secret", global.IL.Secret},
		{"moderated", global.IL.Moderated},
		{"moderation-queue", global.IL.ModerationQueue},
		{"popular", global.IL.Popular},
		{"uppermost", global.IL.Uppermost},
		{"scheduled", global.IL.Scheduled},
		{"invites", global.IL.Invites},
		{"pending-access", global.IL.PendingAccess},
		{"blossom", global.IL.Blossom},
		{"operator", global.IL.OperatorBucket},
	}
}

// exportHandler streams a complete relay backup as a .tar.gz (GET /export):
// every event layer as <name>.jsonl, plus settings.json, the membership log,
// grasp git repositories, and (unless ?blossom=0) uploaded blossom media.
func exportHandler(w http.ResponseWriter, r *http.Request) {
	if !exportAuthorized(r) {
		http.Error(w, "unauthorized", 403)
		return
	}
	if global.IL.Main == nil {
		http.Error(w, "event store unavailable", 500)
		return
	}

	// each layer is staged to one reused temp file so we know its size before
	// writing the tar header (tar has no streaming-unknown-size mode)
	tmp, err := os.CreateTemp(global.S.DataPath, ".export-stage-*.jsonl")
	if err != nil {
		http.Error(w, "export staging failed", 500)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	now := time.Now().UTC()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="pyramid-%s-backup-%s.tar.gz"`,
			global.Settings.Domain, now.Format("20060102")))

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	grand := 0
	for _, L := range backupLayers() {
		if L.il == nil {
			continue
		}
		if err := tmp.Truncate(0); err != nil {
			break
		}
		tmp.Seek(0, io.SeekStart)
		bw := bufio.NewWriterSize(tmp, 1<<20)
		n := writeLayerJSONL(bw, L.il)
		bw.Flush()
		if n == 0 {
			continue // don't clutter the archive with empty layers
		}
		info, err := tmp.Stat()
		if err != nil {
			continue
		}
		tmp.Seek(0, io.SeekStart)
		if err := tw.WriteHeader(&tar.Header{
			Name: L.name + ".jsonl", Mode: 0600, Size: info.Size(), ModTime: now,
		}); err != nil {
			break
		}
		io.Copy(tw, tmp)
		grand += n
	}

	// on-disk data referenced by events but stored outside the event layers
	dp := global.S.DataPath
	addBackupFile(tw, filepath.Join(dp, "settings.json"), "settings.json")
	addBackupFile(tw, filepath.Join(dp, "management.jsonl"), "management.jsonl")
	addBackupDir(tw, dp, "grasp-repos") // NIP-34 git repositories
	if r.URL.Query().Get("blossom") != "0" {
		addBackupDir(tw, dp, "blossom-files") // blossom media (largest; opt-out)
	}

	tw.Close()
	gz.Close()
	log.Info().Int("events", grand).Str("domain", global.Settings.Domain).Msg("backup archive served")
}

// addBackupFile tars an on-disk file if it exists; missing files are skipped.
func addBackupFile(tw *tar.Writer, path, name string) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}
	addBackupFileInfo(tw, path, name, info)
}

// addBackupDir tars every file under <dataPath>/<name>, preserving paths
// relative to dataPath (so entries are e.g. grasp-repos/<pubkey>/<repo>/...).
// A missing directory is skipped.
func addBackupDir(tw *tar.Writer, dataPath, name string) {
	dir := filepath.Join(dataPath, name)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return
	}
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dataPath, path)
		if err != nil {
			return nil
		}
		addBackupFileInfo(tw, path, filepath.ToSlash(rel), info)
		return nil
	})
}

func addBackupFileInfo(tw *tar.Writer, path, name string, info os.FileInfo) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if err := tw.WriteHeader(&tar.Header{
		Name: name, Mode: 0600, Size: info.Size(), ModTime: info.ModTime(),
	}); err != nil {
		return
	}
	io.Copy(tw, f)
}

func writeLayerJSONL(w *bufio.Writer, layer *mmm.IndexingLayer) int {
	total := 0
	for evt := range layer.QueryEvents(nostr.Filter{}, math.MaxInt32) {
		b, err := evt.MarshalJSON()
		if err != nil {
			continue
		}
		w.Write(b)
		w.WriteByte('\n')
		total++
	}
	return total
}
