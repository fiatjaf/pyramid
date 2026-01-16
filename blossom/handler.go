package blossom

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/blossom"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log        = global.Log.With().Str("module", "blossom").Logger()
	Handler    = &MuxHandler{}
	hostRelay  *khatru.Relay // hack to get the main relay object into here
	blobDir    string
	BlobIndex  blossom.EventStoreBlobIndexWrapper
	Server     *blossom.BlossomServer
	serviceURL string
)

func Init(relay *khatru.Relay) {
	hostRelay = relay
	blobDir = filepath.Join(global.S.DataPath, "blossom-files")
	serviceURL = global.Settings.HTTPScheme() + global.Settings.Domain
	BlobIndex = blossom.EventStoreBlobIndexWrapper{
		Store:      global.IL.Blossom,
		ServiceURL: serviceURL,
	}

	if !global.Settings.Blossom.Enabled {
		setupDisabled()
	} else {
		setupEnabled()
	}
}

func setupDisabled() {
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /blossom/enable", enableHandler)
	Handler.mux.HandleFunc("/blossom/", pageHandler)
}

func setupEnabled() {
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		log.Error().Err(err).Msg("failed to create blossom directory")
		return
	}

	Server = blossom.New(hostRelay, serviceURL)
	Server.Store = BlobIndex

	Server.StoreBlob = func(ctx context.Context, sha256 string, ext string, body []byte) error {
		return os.WriteFile(filepath.Join(blobDir, sha256+ext), body, 0644)
	}
	Server.LoadBlob = func(ctx context.Context, sha256 string, ext string) (io.ReadSeeker, *url.URL, error) {
		file, err := os.Open(filepath.Join(blobDir, sha256+ext))
		if err != nil {
			return nil, nil, err
		}
		return file, nil, nil
	}
	Server.DeleteBlob = func(ctx context.Context, sha256 string, ext string) error {
		return os.Remove(filepath.Join(blobDir, sha256+ext))
	}

	Server.RejectUpload = func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
		if auth == nil {
			return true, "authentication required", 401
		}
		if !pyramid.IsMember(auth.PubKey) {
			return true, "only pyramid members can upload blobs", 403
		}

		// check user upload size limit
		maxSize := global.Settings.Blossom.MaxUserUploadSize * 1024 * 1024
		if maxSize > 0 {
			if size > maxSize {
				return true, "upload by itself exceeds user storage limit", 413
			}

			total := 0
			for blob := range BlobIndex.List(ctx, auth.PubKey) {
				total += blob.Size
				if total+size > maxSize {
					return true, "upload would exceed user storage limit", 413
				}
			}
		}

		return false, "", 0
	}

	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /blossom/disable", disableHandler)
	Handler.mux.HandleFunc("/blossom/", pageHandler)
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	blossomPage(loggedUser).Render(r.Context(), w)
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Blossom.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/blossom/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Blossom.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/blossom/", 302)
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}
