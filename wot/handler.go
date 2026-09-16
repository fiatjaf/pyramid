package wot

import (
	"fmt"
	"net/http"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

func PageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	page(loggedUser).Render(r.Context(), w)
}

func CheckHandler(w http.ResponseWriter, r *http.Request) {
	caller, ok := global.GetLoggedUser(r)
	if !ok || !pyramid.IsMember(caller) {
		http.Error(w, "unauthorized", 403)
		return
	}

	pubkeyInput := r.FormValue("pubkey")
	if pubkeyInput == "" {
		http.Error(w, "pubkey parameter required", 400)
		return
	}

	pk := global.PubKeyFromInput(pubkeyInput)
	if pk == nostr.ZeroPK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"error": "%s"}`, "invalid pubkey")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, "%v", Contains(pk))
}
