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

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
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
		settings := global.Settings
		r.ParseForm()

		for k, v := range r.PostForm {
			switch k {
			case "browse_uri":
				settings.BrowseURI = v[0]
			case "background_color":
				settings.BackgroundColor = v[0]
			case "text_color":
				settings.TextColor = v[0]
			case "accent_color":
				settings.AccentColor = v[0]
			case "relay_name":
				settings.RelayName = v[0]
			case "relay_description":
				settings.RelayDescription = v[0]
			case "relay_contact":
				settings.RelayContact = v[0]
			case "relay_icon":
				settings.RelayIcon = v[0]
			case "max_invites_per_person":
				if maxInvites, err := strconv.Atoi(v[0]); err == nil && maxInvites > 0 {
					settings.MaxInvitesPerPerson = maxInvites
				}
			case "require_current_timestamp":
				settings.RequireCurrentTimestamp = v[0] == "on"
			}
		}

		if err := global.SaveUserSettings(settings); err != nil {
			http.Error(w, "failed to save config: "+err.Error(), 500)
			return
		}

		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			if r.Referer() != "" && strings.Contains(r.Referer(), "/groups") {
				http.Redirect(w, r, "/groups", 302)
			} else {
				http.Redirect(w, r, "/settings", 302)
			}
		}
		return
	}

	settingsPage(loggedUser).Render(r.Context(), w)
}

func uploadIconHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
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
		ext = "png"
	case "image/jpeg", "image/jpg":
		ext = "jpg"
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
	iconPath := filepath.Join(global.S.DataPath, "icon."+ext)
	if err := os.WriteFile(iconPath, fileBytes, 0644); err != nil {
		http.Error(w, "failed to save file", 500)
		return
	}

	// remove old icon file if different extension
	otherExt := "jpg"
	if ext == "jpg" {
		otherExt = "png"
	}
	os.Remove(filepath.Join(global.S.DataPath, "icon."+otherExt))

	// update settings with new icon URL
	settings := global.Settings
	settings.RelayIcon = r.Header.Get("Origin") + "/icon." + ext
	if err := global.SaveUserSettings(settings); err != nil {
		http.Error(w, "failed to update settings", 500)
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/settings", 302)
	}
}

func enableGroupsHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	settings := global.Settings
	settings.Groups.SecretKey = nostr.Generate()

	if err := global.SaveUserSettings(settings); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	http.Redirect(w, r, "/groups", 302)
}

func iconHandler(w http.ResponseWriter, r *http.Request) {
	ext := r.URL.Path[len("/icon."):]
	if ext != "png" && ext != "jpg" {
		http.NotFound(w, r)
		return
	}

	iconPath := filepath.Join(global.S.DataPath, "icon."+ext)
	if _, err := os.Stat(iconPath); os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}

	contentType := "image/png"
	if ext == "jpg" {
		contentType = "image/jpeg"
	}
	w.Header().Set("Content-Type", contentType)

	http.ServeFile(w, r, iconPath)
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
