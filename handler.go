package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/nip05"
	"fiatjaf.com/nostr/nip19"

	"github.com/fiatjaf/pyramid/favorites"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/inbox"
	"github.com/fiatjaf/pyramid/internal"
	"github.com/fiatjaf/pyramid/moderated"
	"github.com/fiatjaf/pyramid/popular"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/fiatjaf/pyramid/uppermost"
)

func inviteTreeHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	var nip05Names map[nostr.PubKey]string
	if global.Settings.NIP05.Enabled {
		nip05Names = make(map[nostr.PubKey]string, pyramid.Members.Size())
		for name, pubkey := range global.Settings.NIP05.Names {
			nip05Names[pubkey] = name
		}
	}
	inviteTreePage(loggedUser, nip05Names).Render(r.Context(), w)
}

func actionHandler(w http.ResponseWriter, r *http.Request) {
	var type_ pyramid.Action
	switch r.PostFormValue("type") {
	case pyramid.ActionInvite:
		type_ = pyramid.ActionInvite
	case pyramid.ActionDrop:
		type_ = pyramid.ActionDrop
	case pyramid.ActionLeave:
		type_ = pyramid.ActionLeave
	}
	author, _ := global.GetLoggedUser(r)
	target := global.PubKeyFromInput(r.PostFormValue("target"))

	if err := pyramid.AddAction(type_, author, target); err != nil {
		http.Error(w, err.Error(), 403)
		return
	}

	go publishMembershipChange(target, type_ == pyramid.ActionInvite)
	http.Redirect(w, r, "/", 302)
}

func settingsHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	if r.Method == http.MethodPost {
		r.ParseForm()

		var delayedRedirectTarget string
		for k, v := range r.PostForm {
			v[0] = strings.TrimSpace(v[0])

			switch k {
			case "domain":
				if err := setupDomain(v[0]); err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				//
				// theme settings
			case "background_color":
				global.Settings.Theme.BackgroundColor = v[0]
			case "text_color":
				global.Settings.Theme.TextColor = v[0]
			case "accent_color":
				global.Settings.Theme.AccentColor = v[0]
			case "secondary_background_color":
				global.Settings.Theme.SecondaryBackgroundColor = v[0]
			case "extra_color":
				global.Settings.Theme.ExtraColor = v[0]
			case "base_color":
				global.Settings.Theme.BaseColor = v[0]
			case "header_transparency":
				global.Settings.Theme.HeaderTransparency = v[0]
			case "primary_font":
				global.Settings.Theme.PrimaryFont = v[0]
			case "secondary_font":
				global.Settings.Theme.SecondaryFont = v[0]
				//
				// general settings
			case "max_invites_per_person":
				global.Settings.MaxInvitesPerPerson, _ = strconv.Atoi(v[0])
			case "browse_uri":
				global.Settings.BrowseURI = v[0]
			case "link_url":
				global.Settings.LinkURL = v[0]
			case "require_current_timestamp":
				global.Settings.RequireCurrentTimestamp = v[0] == "on"
			case "enable_ots":
				global.Settings.EnableOTS = v[0] == "on"
			case "accept_future_events":
				global.Settings.AcceptScheduledEvents = v[0] == "on"
			case "paywall_tag":
				global.Settings.Paywall.Tag = v[0]
			case "paywall_amount":
				if amt, err := strconv.ParseUint(v[0], 10, 64); err == nil {
					global.Settings.Paywall.AmountSats = uint(amt)
				}
			case "paywall_period":
				if days, err := strconv.ParseUint(v[0], 10, 64); err == nil {
					global.Settings.Paywall.PeriodDays = uint(days)
				}
				//
				// nip-05 settings
			case "nip05_enabled":
				global.Settings.NIP05.Enabled = v[0] == "on"
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
			case "favorites_httpBasePath":
				if len(v[0]) == 0 || !justLetters.MatchString(v[0]) {
					http.Error(w, "invalid path must contain only ascii letters and numbers", 400)
					return
				}
				global.Settings.Favorites.HTTPBasePath = v[0]
				favorites.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + v[0]
				delayedRedirectTarget = global.Settings.HTTPScheme() + global.Settings.Domain + "/" + v[0] + "/"
				go restartSoon()
			case "moderated_name":
				global.Settings.Moderated.Name = v[0]
			case "moderated_description":
				global.Settings.Moderated.Description = v[0]
			case "moderated_icon":
				global.Settings.Moderated.Icon = v[0]
			case "moderated_httpBasePath":
				if len(v[0]) > 0 {
					global.Settings.Moderated.HTTPBasePath = v[0]
					moderated.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + v[0]
					delayedRedirectTarget = global.Settings.HTTPScheme() + global.Settings.Domain + "/" + v[0] + "/"
					go restartSoon()
				}
			case "inbox_name":
				global.Settings.Inbox.Name = v[0]
			case "inbox_description":
				global.Settings.Inbox.Description = v[0]
			case "inbox_icon":
				global.Settings.Inbox.Icon = v[0]
			case "inbox_httpBasePath":
				if len(v[0]) == 0 || !justLetters.MatchString(v[0]) {
					http.Error(w, "invalid path must contain only ascii letters and numbers", 400)
					return
				}
				global.Settings.Inbox.HTTPBasePath = v[0]
				inbox.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + v[0]
				delayedRedirectTarget = global.Settings.HTTPScheme() + global.Settings.Domain + "/" + v[0] + "/"
				go restartSoon()
			case "internal_name":
				global.Settings.Internal.Name = v[0]
			case "internal_description":
				global.Settings.Internal.Description = v[0]
			case "internal_icon":
				global.Settings.Internal.Icon = v[0]
			case "internal_httpBasePath":
				if len(v[0]) == 0 || !justLetters.MatchString(v[0]) {
					http.Error(w, "invalid path must contain only ascii letters and numbers", 400)
					return
				}
				global.Settings.Internal.HTTPBasePath = v[0]
				internal.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + v[0]
				delayedRedirectTarget = global.Settings.HTTPScheme() + global.Settings.Domain + "/" + v[0] + "/"
				go restartSoon()
			case "popular_name":
				global.Settings.Popular.Name = v[0]
			case "popular_description":
				global.Settings.Popular.Description = v[0]
			case "popular_icon":
				global.Settings.Popular.Icon = v[0]
			case "popular_httpBasePath":
				if len(v[0]) == 0 || !justLetters.MatchString(v[0]) {
					http.Error(w, "invalid path must contain only ascii letters and numbers", 400)
					return
				}
				global.Settings.Popular.HTTPBasePath = v[0]
				popular.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + v[0]
				delayedRedirectTarget = global.Settings.HTTPScheme() + global.Settings.Domain + "/" + v[0] + "/"
				go restartSoon()
			case "uppermost_name":
				global.Settings.Uppermost.Name = v[0]
			case "uppermost_description":
				global.Settings.Uppermost.Description = v[0]
			case "uppermost_icon":
				global.Settings.Uppermost.Icon = v[0]
			case "uppermost_httpBasePath":
				if len(v[0]) == 0 || !justLetters.MatchString(v[0]) {
					http.Error(w, "invalid path must contain only ascii letters and numbers", 400)
					return
				}
				global.Settings.Uppermost.HTTPBasePath = v[0]
				uppermost.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + v[0]
				delayedRedirectTarget = global.Settings.HTTPScheme() + global.Settings.Domain + "/" + v[0] + "/"
				go restartSoon()
				//
				// moderated-specific
			case "moderated_enabled":
				global.Settings.Moderated.Enabled = v[0] == "on"
			case "moderated_min_pow":
				pow, _ := strconv.ParseUint(v[0], 10, 64)
				global.Settings.Moderated.MinPoW = uint(pow)
				//
				// inbox-specific
			case "inbox_hellthread_limit":
				global.Settings.Inbox.HellthreadLimit, _ = strconv.Atoi(v[0])
			case "inbox_min_dm_pow":
				global.Settings.Inbox.MinDMPoW, _ = strconv.Atoi(v[0])
			case "inbox_specifically_blocked":
				var blocked []nostr.PubKey
				for _, s := range v {
					s = strings.TrimSpace(s)
					if s == "" {
						continue
					}
					pk := global.PubKeyFromInput(s)
					if pk != nostr.ZeroPK && !slices.Contains(blocked, pk) {
						blocked = append(blocked, pk)
					}
				}
				global.Settings.Inbox.SpecificallyBlocked = blocked
				//
				// popular-specific
			case "popular_percent_threshold":
				if val, err := strconv.Atoi(v[0]); err == nil {
					global.Settings.Popular.PercentThreshold = val
				}
				//
				// uppermost-specific
			case "uppermost_percent_threshold":
				if val, err := strconv.Atoi(v[0]); err == nil {
					global.Settings.Uppermost.PercentThreshold = val
				}
			}
		}

		if err := global.SaveUserSettings(); err != nil {
			http.Error(w, "failed to save config: "+err.Error(), 500)
			return
		}

		if delayedRedirectTarget != "" {
			r.Header.Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!doctype html><meta http-equiv="refresh" content="2;url=`+delayedRedirectTarget+`">restarting...`)
			return
		}

		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, r.Header.Get("Referer"), 302)
		}

		return
	}

	settingsPage(loggedUser).Render(r.Context(), w)
}

func iconHandler(w http.ResponseWriter, r *http.Request) {
	// this will be either a relay name like "favorites" or it will have an extension like "favorites.png"
	relayId := r.PathValue("relayId")

	spl := strings.Split(relayId, ".")
	base := spl[0]

	switch r.Method {
	case "GET":
		for _, ext := range []string{".png", ".jpeg"} {
			path := filepath.Join(global.S.DataPath, base+ext)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				continue
			}

			contentType := "image/png"
			if ext == ".jpeg" {
				contentType = "image/jpeg"
			}

			w.Header().Set("Content-Type", contentType)
			http.ServeFile(w, r, path)
			return
		}

		// if it's not .png or .jpeg:
		http.NotFound(w, r)
		return

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
			global.Settings.RelayIcon = global.Settings.HTTPScheme() + global.Settings.Domain + "/icon/" + base + ext
		case "favorites":
			global.Settings.Favorites.Icon = global.Settings.HTTPScheme() + global.Settings.Domain + "/icon/" + base + ext
		case "inbox":
			global.Settings.Inbox.Icon = global.Settings.HTTPScheme() + global.Settings.Domain + "/icon/" + base + ext
		case "internal":
			global.Settings.Internal.Icon = global.Settings.HTTPScheme() + global.Settings.Domain + "/icon/" + base + ext
		case "popular":
			global.Settings.Popular.Icon = global.Settings.HTTPScheme() + global.Settings.Domain + "/icon/" + base + ext
		case "uppermost":
			global.Settings.Uppermost.Icon = global.Settings.HTTPScheme() + global.Settings.Domain + "/icon/" + base + ext
		case "moderated":
			global.Settings.Moderated.Icon = global.Settings.HTTPScheme() + global.Settings.Domain + "/icon/" + base + ext
		}

		if err := global.SaveUserSettings(); err != nil {
			http.Error(w, "failed to update settings", 500)
			return
		}

		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, r.Header.Get("Referer"), 302)
		}
	}
}

var domainRegex = regexp.MustCompile(`^((xn--|_)?[a-z0-9-]{0,61}[a-z0-9]{1,1}\.)*(xn--)?([a-z0-9][a-z0-9\-]{0,60}|[a-z0-9-]{1,30}\.[a-z]{2,})(:\d{1,5})?$`)

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

		if err := setupDomain(domain); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		if err := global.SaveUserSettings(); err != nil {
			http.Error(w, "failed to save domain: "+err.Error(), 500)
			return
		}

		http.Redirect(w, r, "/", 302)
		return
	}

	domainSetupPage().Render(r.Context(), w)
}

func setupDomain(domain string) error {
	// trim protocol prefixes
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "ws://")
	domain = strings.TrimPrefix(domain, "wss://")

	// trim trailing slashes and spaces again
	domain = strings.TrimRight(domain, "/")
	domain = strings.TrimSpace(domain)

	// validate domain only contains letters, dots, and colons
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("invalid domain format: only letters, dots, and colons are allowed")
	}

	global.Settings.Domain = domain
	relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain

	log.Info().
		Str("domain", global.Settings.Domain).
		Str("service-url", relay.ServiceURL).
		Msg("main relay domain changed")

	inbox.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Inbox.HTTPBasePath
	favorites.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Favorites.HTTPBasePath
	internal.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Internal.HTTPBasePath
	moderated.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Moderated.HTTPBasePath
	popular.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Popular.HTTPBasePath
	uppermost.Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Uppermost.HTTPBasePath

	go restartSoon()
	return nil
}

func rootUserSetupHandler(w http.ResponseWriter, r *http.Request) {
	if pyramid.HasRootUsers() {
		http.Redirect(w, r, "/", 302)
		return
	}

	if r.Method == http.MethodPost {
		pubkeyStr := r.PostFormValue("pubkey")
		target := global.PubKeyFromInput(pubkeyStr)

		if target == nostr.ZeroPK {
			http.Error(w, "invalid public key", 400)
			return
		}

		if err := pyramid.AddAction("invite", pyramid.AbsoluteKey, target); err != nil {
			http.Error(w, "failed to add root user: "+err.Error(), 500)
			return
		}

		http.Redirect(w, r, "/", 302)
		return
	}

	rootUserSetupPage().Render(r.Context(), w)
}

func updateHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	if r.Method == http.MethodPost {
		// if the update is successful the process will restart so this function will never return
		if err := performUpdateInPlace(); err != nil {
			log.Error().Err(err).Msg("update failed")
			http.Error(w, err.Error(), 500)
			return
		}

		// if we reach here, the update failed to restart
		http.Error(w, "unexpected: update done, but couldn't restart the server (or something else)", 500)
		return
	}
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

func memberPageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, isLogged := global.GetLoggedUser(r)

	if r.Method == http.MethodPost {
		if nip05Username := r.PostFormValue("nip05_username"); nip05Username != "" {
			// basic validation for NIP-05 username (alphanumeric and underscores only)
			nip05Username = strings.ToLower(nip05Username)
			if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(nip05Username) {
				http.Error(w, "invalid username: only letters, numbers, and underscores are allowed", 400)
				return
			}

			// check if this name is already being used
			if _, inUse := global.Settings.NIP05.Names[nip05Username]; inUse {
				http.Error(w, "username already taken", 400)
				return
			}

			// clear the previous username for this user
			for name, pubkey := range global.Settings.NIP05.Names {
				if pubkey == loggedUser {
					delete(global.Settings.NIP05.Names, name)
				}
			}

			// add this new name
			global.Settings.NIP05.Names[nip05Username] = loggedUser

			// save
			global.SaveUserSettings()
		}

		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, r.Header.Get("Referer"), 302)
		}
		return
	}

	var user nostr.PubKey

	pubkeyHex := r.PathValue("pubkey")
	if pubkeyHex == "" && isLogged {
		http.Redirect(w, r, "/u/"+nip19.EncodeNpub(loggedUser), 302)
		return
	} else if pubkeyHex != "" {
		user = global.PubKeyFromInput(pubkeyHex)
		if user == nostr.ZeroPK {
			http.Error(w, "invalid pubkey", 400)
			return
		}
	} else {
		http.Redirect(w, r, "/", 302)
		return
	}

	var nip05 string
	for name, pubkey := range global.Settings.NIP05.Names {
		if pubkey == loggedUser {
			nip05 = name
			break
		}
	}

	// compute user-specific stats
	var mainStats mmm.EventStats
	if pyramid.IsMember(loggedUser) {
		mainStats, _ = global.IL.Main.ComputeStats(mmm.StatsOptions{OnlyPubKey: user})
	}

	memberPage(loggedUser, user, nip05, mainStats).Render(r.Context(), w)
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsMember(loggedUser) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// compute stats for all IndexingLayer instances
	mainStats, _ := global.IL.Main.ComputeStats(mmm.StatsOptions{})
	systemStats, _ := global.IL.System.ComputeStats(mmm.StatsOptions{})
	groupsStats, _ := global.IL.Groups.ComputeStats(mmm.StatsOptions{})
	favoritesStats, _ := global.IL.Favorites.ComputeStats(mmm.StatsOptions{})
	internalStats, _ := global.IL.Internal.ComputeStats(mmm.StatsOptions{})
	moderatedStats, _ := global.IL.Moderated.ComputeStats(mmm.StatsOptions{})
	popularStats, _ := global.IL.Popular.ComputeStats(mmm.StatsOptions{})
	uppermostStats, _ := global.IL.Uppermost.ComputeStats(mmm.StatsOptions{})
	inboxStats, _ := global.IL.Inbox.ComputeStats(mmm.StatsOptions{})

	StatsPage(loggedUser, mainStats, systemStats, groupsStats, favoritesStats, internalStats, moderatedStats, popularStats, uppermostStats, inboxStats).Render(r.Context(), w)
}

func syncHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsMember(loggedUser) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	remoteUrl := r.FormValue("remote")
	download := r.FormValue("download") == "on"
	upload := r.FormValue("upload") == "on"

	streamingSync(r.Context(), loggedUser, remoteUrl, download, upload, w)
}

func nip05Handler(w http.ResponseWriter, r *http.Request) {
	resp := nip05.WellKnownResponse{
		Names: global.Settings.NIP05.Names,
	}

	specifiedNames := r.URL.Query()["name"]
	if len(specifiedNames) > 0 {
		resp.Relays = make(map[nostr.PubKey][]string, len(specifiedNames))
	}
	for _, name := range specifiedNames {
		if pk, ok := global.Settings.NIP05.Names[name]; ok {
			resp.Relays[pk] = []string{global.Settings.WSScheme() + global.Settings.Domain}
		}
	}
	json.NewEncoder(w).Encode(resp)
}
