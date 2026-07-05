package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"fiatjaf.com/nostr"

	"github.com/fiatjaf/pyramid/global"
)

// Restore is the reverse of the /export backup. A .tar.gz dropped at
// <DataPath>/.restore.tar.gz is applied on the next boot: the relay becomes the
// backup — settings.json (incl. the internal signing key, so nip-29 groups keep
// their identity) and management.jsonl replace the current ones, grasp repos and
// blossom media are extracted to disk, and each <layer>.jsonl is imported into
// its mmm layer (additive, dedup by id). The host domain is independently
// re-asserted from PYRAMID_DOMAIN on every boot (see bootstrap.go), so a backup
// from another host can never break login.
//
// Applying requires a REAL process restart: settings.json is read inside
// global.Init and management.jsonl by pyramid.LoadManagement, while event layers
// need the store open — hence the two phases below, run from main(). The
// settings-page restart button does an in-process soft restart and will NOT
// apply a staged restore. The whole thing is idempotent (file overwrites +
// SaveEvent dedup), so an interrupted restore simply retries on the next boot
// and converges. The relay does not serve until the import completes — it is
// honestly "down, restoring", with progress in the log.

const (
	restoreArchiveName = ".restore.tar.gz"
	restoreStageName   = ".restore-stage"
	restoreFailedName  = ".restore.failed"
)

// restorePreInit runs as the very first thing in main(), BEFORE global.Init.
// global.S is not populated yet, so it reads DATA_PATH from the env directly.
// It extracts the on-disk companions (settings.json, management.jsonl, grasp
// repos, blossom media) into place so global.Init / LoadManagement pick them up,
// and stages the event-layer jsonls for restorePostInit. A corrupt archive is
// renamed aside so it never loops.
func restorePreInit() {
	dataPath := os.Getenv("DATA_PATH")
	if dataPath == "" {
		dataPath = "./data"
	}
	archivePath := filepath.Join(dataPath, restoreArchiveName)
	if _, err := os.Stat(archivePath); err != nil {
		return // nothing to restore
	}
	fmt.Fprintf(os.Stderr, "restore: found %s, applying\n", archivePath)

	stageDir := filepath.Join(dataPath, restoreStageName)
	os.RemoveAll(stageDir)
	if err := os.MkdirAll(stageDir, 0700); err != nil {
		restoreAbort(archivePath, dataPath, fmt.Errorf("create stage dir: %w", err))
		return
	}

	f, err := os.Open(archivePath)
	if err != nil {
		restoreAbort(archivePath, dataPath, err)
		return
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		restoreAbort(archivePath, dataPath, fmt.Errorf("not gzip: %w", err))
		return
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			restoreAbort(archivePath, dataPath, fmt.Errorf("read archive: %w", err))
			return
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		name := filepath.Clean(hdr.Name)
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue // ignore anything trying to escape the data dir
		}

		// root-level <layer>.jsonl is staged for the post-init import; every
		// other entry (settings.json, management.jsonl, grasp-repos/**,
		// blossom-files/**) is written straight into the data dir
		var dest string
		if isLayerFile(name) {
			dest = filepath.Join(stageDir, name)
		} else {
			dest = filepath.Join(dataPath, name)
		}
		if !withinDir(dataPath, dest) {
			continue
		}
		if err := writeTarEntry(dest, tr, hdr); err != nil {
			restoreAbort(archivePath, dataPath, err)
			return
		}
	}
	fmt.Fprintln(os.Stderr, "restore: companions extracted, event layers staged")
}

// restorePostInit runs after global.Init (store open), before the relay serves.
// It imports each staged <layer>.jsonl into its mmm layer and, on success,
// removes the staging dir and the archive so the next boot is normal.
func restorePostInit() {
	dataPath := global.S.DataPath
	stageDir := filepath.Join(dataPath, restoreStageName)
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return // no restore in progress
	}

	log.Info().Msg("restore: importing event layers (the relay serves once this completes)")
	total := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		layerName := strings.TrimSuffix(e.Name(), ".jsonl")
		il, err := global.MMMM.EnsureLayer(layerName)
		if err != nil {
			log.Error().Err(err).Str("layer", layerName).Msg("restore: cannot open layer")
			continue
		}
		n, err := importLayerJSONL(filepath.Join(stageDir, e.Name()), il, layerName)
		if err != nil {
			log.Error().Err(err).Str("layer", layerName).Msg("restore: import failed")
			continue
		}
		total += n
		log.Info().Str("layer", layerName).Int("new_events", n).Msg("restore: imported layer")
	}

	os.RemoveAll(stageDir)
	os.Remove(filepath.Join(dataPath, restoreArchiveName))
	log.Info().Int("new_events", total).Msg("restore: complete")
}

// importLayerJSONL reads one <layer>.jsonl and SaveEvents each line, skipping
// events that already exist. Returns the number of new events stored. mmm
// fsyncs every SaveEvent, so this is slow for large layers — progress is logged
// so operators can watch the restore advance.
func importLayerJSONL(path string, il layerSaver, layerName string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20) // events can exceed the 64KB default line cap
	stored, seen := 0, 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var evt nostr.Event
		if err := json.Unmarshal(line, &evt); err != nil {
			continue // skip a malformed line rather than abort the whole layer
		}
		// count only newly-stored events; ErrDupEvent (already present, no fsync)
		// and any other error are left as-is — restore is additive and idempotent
		if err := il.SaveEvent(evt); err == nil {
			stored++
		}
		if seen++; seen%5000 == 0 {
			log.Info().Str("layer", layerName).Int("processed", seen).Int("new", stored).Msg("restore: importing")
		}
	}
	return stored, sc.Err()
}

// layerSaver is the slice of *mmm.IndexingLayer we need, kept small so the
// import is easy to reason about (and test).
type layerSaver interface {
	SaveEvent(nostr.Event) error
}

// restoreAbort renames a bad archive aside and clears the stage so a corrupt or
// truncated upload can never send the relay into a restart loop.
func restoreAbort(archivePath, dataPath string, cause error) {
	fmt.Fprintf(os.Stderr, "restore: aborting (%s); renaming archive to %s\n", cause, restoreFailedName)
	os.RemoveAll(filepath.Join(dataPath, restoreStageName))
	_ = os.Rename(archivePath, filepath.Join(dataPath, restoreFailedName))
}

func writeTarEntry(dest string, tr *tar.Reader, hdr *tar.Header) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0777|0600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, tr); err != nil {
		return err
	}
	return nil
}

// isLayerFile reports whether a root-level archive entry is an event-layer dump
// (<name>.jsonl imported into an mmm layer) rather than a companion file.
// management.jsonl is the membership log, not a layer, so it is excluded and
// restored to disk like settings.json.
func isLayerFile(name string) bool {
	return !strings.ContainsRune(name, '/') &&
		strings.HasSuffix(name, ".jsonl") &&
		name != "management.jsonl"
}

// withinDir reports whether path is inside dir (a defence against ../ escapes).
func withinDir(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// -------- upload endpoint --------

// restoreHandler stages an uploaded backup and restarts the relay to apply it.
// It is the write-side counterpart of exportHandler and shares its auth: an
// operator token or a logged-in root session (POST /restore, field "file").
func restoreHandler(w http.ResponseWriter, r *http.Request) {
	if !exportAuthorized(r) {
		http.Error(w, "unauthorized", 403)
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected a multipart form upload", 400)
		return
	}

	dataPath := global.S.DataPath
	tmp, err := os.CreateTemp(dataPath, ".restore-upload-*")
	if err != nil {
		http.Error(w, "could not stage upload", 500)
		return
	}
	tmpPath := tmp.Name()
	keepTmp := false
	defer func() {
		if !keepTmp {
			os.Remove(tmpPath)
		}
	}()

	// stream the "file" part straight to disk — backups can be very large
	got := false
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			tmp.Close()
			http.Error(w, "could not read upload", 400)
			return
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		if _, err := io.Copy(tmp, part); err != nil {
			part.Close()
			tmp.Close()
			http.Error(w, "could not write upload", 500)
			return
		}
		part.Close()
		got = true
		break
	}
	tmp.Close()
	if !got {
		http.Error(w, `no "file" field in upload`, 400)
		return
	}

	// never restart into a bad archive
	if err := validateBackupArchive(tmpPath); err != nil {
		http.Error(w, "invalid backup archive: "+err.Error(), 400)
		return
	}

	if err := os.Rename(tmpPath, filepath.Join(dataPath, restoreArchiveName)); err != nil {
		http.Error(w, "could not stage archive", 500)
		return
	}
	keepTmp = true

	// deliberately no restart here: applying a restore requires a REAL process
	// restart (the relay must come up clean and re-read everything), and that is
	// the operator's move. Note the settings-page restart button does an
	// in-process soft restart and will NOT apply a staged restore.
	log.Info().Msg("restore: archive staged via upload; waiting for a full process restart to apply")
	w.WriteHeader(200)
	fmt.Fprint(w, "backup staged — fully restart the pyramid process to apply it "+
		"(restart the container/service; the settings-page restart button is not a full restart)")
}

// validateBackupArchive cheaply confirms an upload is a gzip'd tar that actually
// looks like one of our backups (has settings.json or at least one event layer).
func validateBackupArchive(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("not a gzip file")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("not a valid tar archive")
		}
		base := filepath.Base(filepath.Clean(hdr.Name))
		if base == "settings.json" || strings.HasSuffix(base, ".jsonl") {
			return nil
		}
	}
	return fmt.Errorf("no settings.json or event layers found")
}

