package linkpreview

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/rs/cors"
	"golang.org/x/net/html"
)

var (
	log     = global.Log.With().Str("service", "linkpreview").Logger()
	Handler = &MuxHandler{}

	client = &http.Client{Timeout: 10 * time.Second}

	baseSecret []byte
)

func Init() {
	baseSecret, _ = hex.DecodeString(global.Settings.LinkPreview.BaseSecret)
	Setup()
}

func Setup() {
	baseSecret, _ = hex.DecodeString(global.Settings.LinkPreview.BaseSecret)
	Handler.mux = http.NewServeMux()
	if global.Settings.LinkPreview.Enabled {
		Handler.mux.HandleFunc("POST /linkpreview/disable", disableHandler)
		Handler.mux.Handle("/linkpreview/secret", cors.AllowAll().Handler(http.HandlerFunc(secretHandler)))
	} else {
		Handler.mux.HandleFunc("POST /linkpreview/enable", enableHandler)
	}
	Handler.mux.HandleFunc("/linkpreview/", pageHandler)
	Handler.mux.HandleFunc("/linkpreview", pageHandler)

	// use this URL pattern to copy dufflepud
	Handler.mux.Handle("POST /link/preview", cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"POST"},
		AllowedHeaders: []string{"*"},
		ExposedHeaders: []string{"*"},
	}).Handler(http.HandlerFunc(previewHandler)))
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}

type Preview struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Image       string `json:"image"`
}

type previewRequest struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/linkpreview/" && r.URL.Path != "/linkpreview" {
		http.NotFound(w, r)
		return
	}

	loggedUser, _ := global.GetLoggedUser(r)
	linkpreviewPage(loggedUser).Render(r.Context(), w)
}

func previewHandler(w http.ResponseWriter, r *http.Request) {
	if !global.Settings.LinkPreview.Enabled {
		http.Error(w, "disabled", http.StatusNotFound)
		return
	}

	var req previewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		http.Error(w, "url must be http(s)", http.StatusBadRequest)
		return
	}

	if req.Token != "" {
		if err := verifyToken(req.Token, req.URL); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
	} else if !global.OriginAllowed(r, global.Settings.LinkPreview.AllowedDomains) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	resp, err := client.Get(req.URL)
	if err != nil {
		http.Error(w, "failed to fetch url", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	preview := Preview{URL: req.URL}
	parseOG(resp.Body, &preview)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(preview)
}

func parseOG(r io.Reader, preview *Preview) {
	doc, err := html.Parse(r)
	if err != nil {
		return
	}

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" {
			var property, content string
			for _, attr := range n.Attr {
				if attr.Key == "property" {
					property = attr.Val
				}
				if attr.Key == "content" {
					content = attr.Val
				}
			}
			if strings.HasPrefix(property, "og:") && content != "" {
				switch property {
				case "og:title":
					if preview.Title == "" {
						preview.Title = content
					}
				case "og:description":
					if preview.Description == "" {
						preview.Description = content
					}
				case "og:image":
					if preview.Image == "" {
						preview.Image = content
					}
				}
			}
		}
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
			if preview.Title == "" {
				preview.Title = strings.TrimSpace(n.FirstChild.Data)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
}

//go:inline
func pubkeySecretFor(pubkey []byte) []byte {
	b := hmac.New(sha256.New, baseSecret)
	b.Write(pubkey)
	return b.Sum(nil)
}

func verifyToken(tokenB64, sourceURL string) error {
	rawToken, err := base64.RawURLEncoding.DecodeString(tokenB64)
	if err != nil || len(rawToken) != 48 {
		return fmt.Errorf("invalid token")
	}

	mac := rawToken[0:16]
	pubkey := rawToken[16 : 16+32]

	pubkeySecret := pubkeySecretFor(pubkey)

	h := hmac.New(sha256.New, pubkeySecret)
	h.Write([]byte(sourceURL))
	expected := h.Sum(nil)
	if !hmac.Equal(expected[0:16], mac) {
		return fmt.Errorf("mac doesn't match")
	}
	return nil
}

func secretHandler(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Nostr ") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	evtj, err := base64.StdEncoding.DecodeString(auth[6:])
	if err != nil {
		http.Error(w, "invalid base64-encoded event", http.StatusUnauthorized)
		return
	}
	var evt nostr.Event
	if err := json.Unmarshal(evtj, &evt); err != nil {
		http.Error(w, "invalid event", http.StatusUnauthorized)
		return
	}
	if evt.Kind != 27235 {
		http.Error(w, "invalid kind", http.StatusUnauthorized)
		return
	}
	if !evt.VerifySignature() {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	if tag := evt.Tags.Find("u"); tag == nil || tag[1] != global.Settings.HTTPScheme()+global.Settings.Domain+r.URL.Path {
		http.Error(w, "invalid url", http.StatusUnauthorized)
		return
	}

	secret := pubkeySecretFor(evt.PubKey[:])
	w.Write([]byte(hex.EncodeToString(secret)))
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	global.Settings.LinkPreview.Enabled = true
	if global.Settings.LinkPreview.BaseSecret == "" {
		secret := make([]byte, 16)
		if _, err := rand.Read(secret); err != nil {
			http.Error(w, "failed to generate secret: "+err.Error(), 500)
			return
		}
		global.Settings.LinkPreview.BaseSecret = hex.EncodeToString(secret)
	}

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}
	Setup()
	http.Redirect(w, r, "/linkpreview/", http.StatusSeeOther)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	global.Settings.LinkPreview.Enabled = false
	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}
	Setup()
	http.Redirect(w, r, "/linkpreview/", http.StatusSeeOther)
}
