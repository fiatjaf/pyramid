package main

import (
	"fmt"
	"net/http"

	"fiatjaf.com/nostr"

	"github.com/fiatjaf/pyramid/global"
	whitelist "github.com/fiatjaf/pyramid/whitelist"
)

func inviteTreeHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := getLoggedUser(r)
	inviteTreePage(loggedUser).Render(r.Context(), w)
}

func actionHandler(w http.ResponseWriter, r *http.Request) {
	type_ := r.PostFormValue("type")
	author, _ := getLoggedUser(r)
	target := pubkeyFromInput(r.PostFormValue("target"))

	if err := whitelist.AddAction(type_, author, target); err != nil {
		http.Error(w, err.Error(), 403)
		return
	}

	inviteTreeComponent(nostr.ZeroPK, author).Render(r.Context(), w)
}

// this deletes all events from users not in the relay anymore
func cleanupStuffFromExcludedUsersHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := getLoggedUser(r)

	if loggedUser != *relay.Info.PubKey {
		http.Error(w, "unauthorized, only the relay owner can do this", 403)
		return
	}

	count := 0
	for evt := range global.Nostr.Store.QueryEvents(nostr.Filter{}, 99999999) {
		if whitelist.IsPublicKeyInWhitelist(evt.PubKey) {
			continue
		}

		if err := global.Nostr.Store.DeleteEvent(evt.ID); err != nil {
			http.Error(w, fmt.Sprintf(
				"failed to delete %s: %s -- stopping, %d events were deleted before this error", evt, err, count), 500)
			return
		}
		count++
	}

	fmt.Fprintf(w, "deleted %d events", count)
}

func reportsViewerHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := getLoggedUser(r)

	events := global.Nostr.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{1984}}, 52)
	reportsPage(events, loggedUser).Render(r.Context(), w)
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
    class="bg-slate-100 transition-colors duration-200 dark:bg-gray-900 dark:text-white"
  >
    <div id="app"></div>
  </body>
  <script src="https://cdn.jsdelivr.net/npm/relay-forum@0.0.2/dist/index.js"></script>
</html>
`)
}
