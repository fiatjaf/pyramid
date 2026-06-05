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
	libblossom "fiatjaf.com/nostr/nipb0/blossom"
	"github.com/fiatjaf/pyramid/blossom"
	"github.com/fiatjaf/pyramid/global"
)

// gatewayHandler serves nsite sites. it is not registered in the mux;
// main.go calls it directly when the host matches an nsite subdomain.
func GatewayHandler(w http.ResponseWriter, r *http.Request) {
	manifest, err := resolveSite(r.Host)
	if err != nil {
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
	pathTag := manifest.Tags.FindWithValue("path", requestPath)
	if len(pathTag) < 3 {
		return fmt.Errorf("path %s not found", requestPath)
	}

	hash := pathTag[2]
	if _, err := hex.DecodeString(hash); err != nil || len(hash) != 64 {
		return fmt.Errorf("invalid hash for %s: %s", requestPath, hash)
	}

	bd, err := blossom.BlobIndex.Get(r.Context(), hash)
	if err != nil {
		log.Warn().Err(err).Str("sha256", hash).Msg("failed to query blossom blob")
		return fmt.Errorf("failed to query blossom blob %s: %w", hash, err)
	}
	if bd == nil {
		return fmt.Errorf("blob not found for %s: %s", requestPath, hash)
	}

	ext := libblossom.GetExtension(bd.Type)
	fp := filepath.Join(global.S.DataPath, "blossom-files", hash+ext)
	file, err := os.Open(fp)
	if err != nil {
		return fmt.Errorf("missing blossom file %s: %w", fp, err)
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
