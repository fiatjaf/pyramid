package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fiatjaf.com/nostr"

	"github.com/fiatjaf/pyramid/favorites"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/groups"
	"github.com/fiatjaf/pyramid/inbox"
	"github.com/fiatjaf/pyramid/internal"
	"github.com/fiatjaf/pyramid/moderated"
	"github.com/fiatjaf/pyramid/popular"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/fiatjaf/pyramid/uppermost"
)

func inviteTreeHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	inviteTreePage(loggedUser).Render(r.Context(), w)
}

func actionHandler(w http.ResponseWriter, r *http.Request) {
	type_ := r.PostFormValue("type")
	author, _ := global.GetLoggedUser(r)
	target := pubkeyFromInput(r.PostFormValue("target"))

	if err := pyramid.AddAction(type_, author, target); err != nil {
		http.Error(w, err.Error(), 403)
		return
	}

	http.Redirect(w, r, "/", 302)
}

// this deletes all events from users not in the relay anymore
func cleanupStuffFromExcludedUsersHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized, only the relay owner can do this", 403)
		return
	}

	count := 0
	for evt := range global.IL.Main.QueryEvents(nostr.Filter{}, 99999999) {
		if pyramid.IsMember(evt.PubKey) {
			continue
		}

		if err := global.IL.Main.DeleteEvent(evt.ID); err != nil {
			http.Error(w, fmt.Sprintf(
				"failed to delete %s: %s -- stopping, %d events were deleted before this error", evt, err, count), 500)
			return
		}
		count++
	}

	fmt.Fprintf(w, "deleted %d events", count)
}

func reportsViewerHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	events := global.IL.Main.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{1984}}, 52)
	reportsPage(events, loggedUser).Render(r.Context(), w)
}

func settingsHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	if r.Method == http.MethodPost {
		r.ParseForm()

		for k, v := range r.PostForm {
			v[0] = strings.TrimSpace(v[0])

			switch k {
			case "domain":
				global.Settings.Domain = v[0]
				root.Relay.ServiceURL = "wss://" + global.Settings.Domain
				inbox.Relay.ServiceURL = "wss://" + global.Settings.Domain + "/inbox"
				favorites.Relay.ServiceURL = "wss://" + global.Settings.Domain + "/favorites"
				groups.Relay.ServiceURL = "wss://" + global.Settings.Domain + "/groups"
				internal.Relay.ServiceURL = "wss://" + global.Settings.Domain + "/internal"
				moderated.Relay.ServiceURL = "wss://" + global.Settings.Domain + "/moderated"
				popular.Relay.ServiceURL = "wss://" + global.Settings.Domain + "/popular"
				uppermost.Relay.ServiceURL = "wss://" + global.Settings.Domain + "/uppermost"
				//
				// theme settings
			case "background_color":
				global.Settings.Theme.BackgroundColor = v[0]
			case "text_color":
				global.Settings.Theme.TextColor = v[0]
			case "accent_color":
				global.Settings.Theme.AccentColor = v[0]
				//
				// general settings
			case "max_invites_per_person":
				global.Settings.MaxInvitesPerPerson, _ = strconv.Atoi(v[0])
			case "browse_uri":
				global.Settings.BrowseURI = v[0]
			case "require_current_timestamp":
				global.Settings.RequireCurrentTimestamp = v[0] == "on"
			case "paywall_tag":
				global.Settings.Paywall.Tag = v[0]
			case "paywall_amount":
				amt, _ := strconv.ParseUint(v[0], 10, 64)
				global.Settings.Paywall.AmountSats = uint(amt)
			case "paywall_period":
				days, _ := strconv.ParseUint(v[0], 10, 64)
				global.Settings.Paywall.PeriodDays = uint(days)
				//
				// enable/disable each relay
			case "favorites_enabled":
				global.Settings.Favorites.Enabled = v[0] == "on"
			case "inbox_enabled":
				global.Settings.Inbox.Enabled = v[0] == "on"
			case "groups_enabled":
				global.Settings.Groups.Enabled = v[0] == "on"
			case "popular_enabled":
				global.Settings.Popular.Enabled = v[0] == "on"
			case "uppermost_enabled":
				global.Settings.Uppermost.Enabled = v[0] == "on"
				//
				// basic metadata of all relays
			case "main_name":
				global.Settings.RelayName = v[0]
			case "main_description":
				global.Settings.RelayDescription = v[0]
			case "main_icon":
				global.Settings.RelayIcon = v[0]
			case "favorites_name":
				global.Settings.Favorites.Name = v[0]
			case "favorites_description":
				global.Settings.Favorites.Description = v[0]
			case "favorites_icon":
				global.Settings.Favorites.Icon = v[0]
			case "inbox_name":
				global.Settings.Inbox.Name = v[0]
			case "inbox_description":
				global.Settings.Inbox.Description = v[0]
			case "inbox_icon":
				global.Settings.Inbox.Icon = v[0]
			case "internal_name":
				global.Settings.Internal.Name = v[0]
			case "internal_description":
				global.Settings.Internal.Description = v[0]
			case "internal_icon":
				global.Settings.Internal.Icon = v[0]
			case "popular_name":
				global.Settings.Popular.Name = v[0]
			case "popular_description":
				global.Settings.Popular.Description = v[0]
			case "popular_icon":
				global.Settings.Popular.Icon = v[0]
			case "uppermost_name":
				global.Settings.Uppermost.Name = v[0]
			case "uppermost_description":
				global.Settings.Uppermost.Description = v[0]
			case "uppermost_icon":
				global.Settings.Uppermost.Icon = v[0]
				//
				// moderated-specific
			case "moderated_enabled":
				global.Settings.Moderated.Enabled = v[0] == "on"
			case "moderated_min_pow":
				pow, _ := strconv.ParseUint(v[0], 10, 64)
				global.Settings.Moderated.MinPoW = uint(pow)
			}
		}

		if err := global.SaveUserSettings(); err != nil {
			http.Error(w, "failed to save config: "+err.Error(), 500)
			return
		}

		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, r.Referer(), 302)
		}

		return
	}

	settingsPage(loggedUser).Render(r.Context(), w)
}

func iconHandler(w http.ResponseWriter, r *http.Request) {
	// this will be either a relay name like "favorites" or it will have an extension like "favorites.png"
	relay := r.PathValue("relay")

	spl := strings.Split(relay, ".")
	base := spl[0]

	switch r.Method {
	case "GET":
		for _, ext := range []string{".png", ".jpeg"} {
			path := filepath.Join(global.S.DataPath, base+ext)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				http.NotFound(w, r)
				return
			}

			contentType := "image/png"
			if ext == ".jpeg" {
				contentType = "image/jpeg"
			}

			w.Header().Set("Content-Type", contentType)

			http.ServeFile(w, r, path)
			return
		}
	case "POST":
		loggedUser, ok := global.GetLoggedUser(r)
		if !ok || !pyramid.IsRoot(loggedUser) {
			http.Error(w, "unauthorized", 403)
			return
		}

		// parse multipart form with 5MB max
		if err := r.ParseMultipartForm(5 << 20); err != nil {
			http.Error(w, "file too large or invalid form", 400)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "no file provided", 400)
			return
		}
		defer file.Close()

		// validate file size
		if header.Size > 5<<20 {
			http.Error(w, "file exceeds 5MB limit", 400)
			return
		}

		// validate content type
		contentType := header.Header.Get("Content-Type")
		var ext string
		switch contentType {
		case "image/png":
			ext = ".png"
		case "image/jpeg", "image/jpg":
			ext = ".jpeg"
		default:
			http.Error(w, "only PNG and JPEG files are allowed", 400)
			return
		}

		// read file content
		fileBytes, err := io.ReadAll(io.LimitReader(file, header.Size))
		if err != nil {
			http.Error(w, "failed to read file", 500)
			return
		}

		// save to data directory
		path := filepath.Join(global.S.DataPath, base+ext)
		if err := os.WriteFile(path, fileBytes, 0644); err != nil {
			http.Error(w, "failed to save file", 500)
			return
		}

		// remove old icon file if different extension
		otherExt := ".jpeg"
		if ext == ".jpeg" {
			otherExt = ".png"
		}
		os.Remove(filepath.Join(global.S.DataPath, base+otherExt))

		// update settings with new icon URL
		switch base {
		case "main":
			global.Settings.RelayIcon = r.Header.Get("Origin") + "/icon/" + base + ext
		case "favorites":
			global.Settings.Favorites.Icon = r.Header.Get("Origin") + "/icon/" + base + ext
		case "inbox":
			global.Settings.Inbox.Icon = r.Header.Get("Origin") + "/icon/" + base + ext
		case "internal":
			global.Settings.Internal.Icon = r.Header.Get("Origin") + "/icon/" + base + ext
		case "popular":
			global.Settings.Popular.Icon = r.Header.Get("Origin") + "/icon/" + base + ext
		case "uppermost":
			global.Settings.Uppermost.Icon = r.Header.Get("Origin") + "/icon/" + base + ext
		case "moderated":
			global.Settings.Moderated.Icon = r.Header.Get("Origin") + "/icon/" + base + ext
		case "groups":
			global.Settings.Groups.Icon = r.Header.Get("Origin") + "/icon/" + base + ext
		}

		if err := global.SaveUserSettings(); err != nil {
			http.Error(w, "failed to update settings", 500)
			return
		}

		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/settings", 302)
		}
	}

}

func domainSetupHandler(w http.ResponseWriter, r *http.Request) {
	if global.Settings.Domain != "" {
		http.Redirect(w, r, "/", 302)
		return
	}

	if r.Method == http.MethodPost {
		domain := strings.TrimSpace(r.PostFormValue("domain"))
		if domain == "" {
			http.Error(w, "domain is required", 400)
			return
		}

		global.Settings.Domain = domain
		if err := global.SaveUserSettings(); err != nil {
			http.Error(w, "failed to save domain: "+err.Error(), 500)
			return
		}

		http.Redirect(w, r, "/", 302)
		return
	}

	domainSetupPage().Render(r.Context(), w)
}

func rootUserSetupHandler(w http.ResponseWriter, r *http.Request) {
	if pyramid.HasRootUsers() {
		http.Redirect(w, r, "/", 302)
		return
	}

	if r.Method == http.MethodPost {
		pubkeyStr := r.PostFormValue("pubkey")
		target := pubkeyFromInput(pubkeyStr)

		if target == nostr.ZeroPK {
			http.Error(w, "invalid public key", 400)
			return
		}

		if err := pyramid.AddAction("invite", nostr.ZeroPK, target); err != nil {
			http.Error(w, "failed to add root user: "+err.Error(), 500)
			return
		}

		http.Redirect(w, r, "/", 302)
		return
	}

	rootUserSetupPage().Render(r.Context(), w)
}

func forumHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `<!doctype html>
<html>
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>forum</title>
    <link rel="preconnect" href="https://fonts.googleapis.com" />
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
    <link
      href="https://fonts.googleapis.com/css2?family=Inter:ital,opsz,wght@0,14..32,100..900;1,14..32,100..900&display=swap"
      rel="stylesheet"
    />
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/relay-forum@0.0.2/dist/index.css" />
    <meta name="base-path" content="/forum" />
  </head>
  <body
    class="bg-slate-100 dark:bg-gray-900 dark:text-white"
  >
    <div id="app"></div>
  </body>
  <script src="https://cdn.jsdelivr.net/npm/relay-forum@0.0.2/dist/index.js"></script>
</html>
`)
}
