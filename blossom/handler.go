package blossom

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/blossom"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/groups"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log       = global.Log.With().Str("service", "blossom").Logger()
	Handler   = &MuxHandler{}
	hostRelay *khatru.Relay // hack to get the main relay object into here
	blobDir   string
	BlobIndex blossom.EventStoreBlobIndexWrapper
	Server    *blossom.BlossomServer
)

func Init(relay *khatru.Relay) {
	hostRelay = relay
	blobDir = filepath.Join(global.S.DataPath, "blossom-files")
	BlobIndex = blossom.EventStoreBlobIndexWrapper{
		Store:      global.IL.Blossom,
		ServiceURL: global.Settings.HTTPScheme() + global.Settings.Domain,
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
	Handler.mux.HandleFunc("GET /blossom/blobs", blobsPageHandler)
	Handler.mux.HandleFunc("GET /blossom/u/{pubkey}", userPageHandler)
	Handler.mux.HandleFunc("DELETE /blossom/b/{sha256}", deleteUserBlobHandler)
	Handler.mux.HandleFunc("/blossom/", pageHandler)
}

func setupEnabled() {
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		log.Error().Err(err).Msg("failed to create blossom directory")
		return
	}

	Server = blossom.New(hostRelay, BlobIndex.ServiceURL)
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
	Server.DeleteBlob = deleteBlob

	Server.RejectUpload = func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
		if auth == nil {
			return true, "authentication required", 401
		}
		isMember := pyramid.IsMember(auth.PubKey)
		isGroupMember := global.Settings.Blossom.AllowGroupMembers &&
			groups.IsMemberOfAnyGroup(auth.PubKey)
		if !isMember && !isGroupMember {
			return true, "only pyramid or group members can upload blobs", 403
		}

		// check user upload size limit
		if !pyramid.IsRoot(auth.PubKey) {
			// Pyramid members use the tree based limit, group-only members use the flat group limit
			var maxSize int
			if isMember {
				maxSize = pyramid.GetMaxBlossomUploadSizeFor(auth.PubKey) * 1024 * 1024
			} else {
				maxSize = global.Settings.Blossom.MaxGroupMemberUploadSize * 1024 * 1024
			}
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
		}

		return false, "", 0
	}

	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /blossom/disable", disableHandler)
	Handler.mux.HandleFunc("GET /blossom/blobs", blobsPageHandler)
	Handler.mux.HandleFunc("GET /blossom/u/{pubkey}", userPageHandler)
	Handler.mux.HandleFunc("DELETE /blossom/b/{sha256}", deleteUserBlobHandler)
	Handler.mux.HandleFunc("/blossom/", pageHandler)
}

func deleteBlob(ctx context.Context, sha256 string, ext string) error {
	entries, err := os.ReadDir(blobDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), sha256) {
			if err := os.Remove(filepath.Join(blobDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	blossomPage(loggedUser).Render(r.Context(), w)
}

func userPageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsMember(loggedUser) {
		http.Error(w, "unauthorized", 401)
		return
	}

	pubkeyInput := r.PathValue("pubkey")
	if pubkeyInput == "" {
		http.Error(w, "missing pubkey", 400)
		return
	}

	user := global.PubKeyFromInput(pubkeyInput)
	if user == nostr.ZeroPK {
		http.Error(w, "invalid pubkey", 400)
		return
	}

	blossomUserPage(loggedUser, user).Render(r.Context(), w)
}

func blobsPageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	blossomAllBlobsPage(loggedUser).Render(r.Context(), w)
}

func deleteUserBlobHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "only root can delete other users' blobs", 403)
		return
	}

	sha256 := r.PathValue("sha256")
	if sha256 == "" {
		http.Error(w, "missing sha256", 400)
		return
	}

	pubkeyInput := r.URL.Query().Get("user")
	targetUser := global.PubKeyFromInput(pubkeyInput)
	if targetUser == nostr.ZeroPK {
		http.Error(w, "invalid pubkey", 400)
		return
	}

	// delete the descriptor for target user
	if err := BlobIndex.Delete(r.Context(), sha256, targetUser); err != nil {
		http.Error(w, "delete failed: "+err.Error(), 500)
		return
	}

	// delete physical file if no other descriptors remain
	if bd, _ := BlobIndex.Get(r.Context(), sha256); bd == nil {
		deleteBlob(r.Context(), sha256, "<irrelevant>")
	}

	w.WriteHeader(200)
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
