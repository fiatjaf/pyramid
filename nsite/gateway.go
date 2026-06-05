package nsite

import (
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
	libblossom "fiatjaf.com/nostr/nipb0/blossom"
	"github.com/fiatjaf/pyramid/blossom"
	"github.com/fiatjaf/pyramid/global"
)

// gatewayHandler serves nsite sites. it is not registered in the mux;
// main.go calls it directly when the host matches an nsite subdomain.
func GatewayHandler(w http.ResponseWriter, r *http.Request) {
	pubkey, identifier, err := resolveSite(r.Host)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	filter := nostr.Filter{Authors: []nostr.PubKey{pubkey}}
	if identifier == "" {
		filter.Kinds = []nostr.Kind{15128}
	} else {
		filter.Kinds = []nostr.Kind{35128}
		filter.Tags = nostr.TagMap{"d": []string{identifier}}
	}

	var manifest nostr.Event
	for evt := range global.IL.Main.QueryEvents(filter, 10) {
		manifest = evt
	}
	if manifest.ID == nostr.ZeroID {
		http.NotFound(w, r)
		return
	}

	requestedPath := sitePath(r.URL.Path)
	if servePath(w, r, manifest, requestedPath) == nil {
		return
	}

	if requestedPath != "/404.html" && servePath(w, r, manifest, "/404.html") == nil {
		return
	}

	http.NotFound(w, r)
}

func sitePath(requestPath string) string {
	hadTrailingSlash := strings.HasSuffix(requestPath, "/")
	requestPath = path.Clean("/" + strings.TrimPrefix(requestPath, "/"))
	if hadTrailingSlash {
		return path.Join(requestPath, "index.html")
	}
	if path.Ext(path.Base(requestPath)) == "" {
		return path.Join(requestPath, "index.html")
	}
	return requestPath
}

func servePath(w http.ResponseWriter, r *http.Request, manifest nostr.Event, requestPath string) error {
	var hash string
	for tag := range manifest.Tags.FindAll("path") {
		if len(tag) >= 3 && tag[1] == requestPath {
			hash = strings.ToLower(tag[2])
			break
		}
	}

	if _, err := hex.DecodeString(hash); err != nil || len(hash) != 64 {
		return errSiteNotFound
	}

	bd, err := blossom.BlobIndex.Get(r.Context(), hash)
	if err != nil {
		log.Warn().Err(err).Str("sha256", hash).Msg("failed to query blossom blob")
		return errSiteNotFound
	}
	if bd == nil {
		return errSiteNotFound
	}

	ext := libblossom.GetExtension(bd.Type)
	file, err := os.Open(filepath.Join(global.S.DataPath, "blossom-files", hash+ext))
	if err != nil {
		return errSiteNotFound
	}
	defer file.Close()

	if bd.Type != "" {
		w.Header().Set("Content-Type", bd.Type)
	} else if contentType := mime.TypeByExtension(path.Ext(requestPath)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if bd.Size > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", bd.Size))
	}
	w.Header().Set("ETag", hash)
	_, err = io.Copy(w, file)
	return err
}

func resolveSite(host string) (nostr.PubKey, string, error) {
	domain := strings.Trim(strings.ToLower(global.Settings.Nsite.Domain), ".")
	if host == domain {
		return nostr.ZeroPK, "", errSiteNotFound
	}

	label := strings.TrimSuffix(host, "."+domain)
	label = strings.TrimSuffix(label, ".")
	if label == "" || strings.Contains(label, ".") {
		return nostr.ZeroPK, "", errSiteNotFound
	}

	if prefix, value, err := nip19.Decode(label); err == nil && prefix == "npub" {
		if pubkey, ok := value.(nostr.PubKey); ok {
			return pubkey, "", nil
		}
	}

	pubkey, err := decodePubkeyB36(label[:50])
	if err != nil {
		return nostr.ZeroPK, "", errSiteNotFound
	}
	return pubkey, label[50:], nil
}
