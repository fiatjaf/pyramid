package paywall

import (
	"net/http"

	"fiatjaf.com/nostr/khatru"

	"github.com/fiatjaf/pyramid/global"
)

var (
	log     = global.Log.With().Str("service", "paywall").Logger()
	Handler = &MuxHandler{}
)

func Init(relay *khatru.Relay) {
	// no special initialization needed for now
}

func pageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	paywallPage(loggedUser).Render(r.Context(), w)
}

type MuxHandler struct{}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pageHandler(w, r)
}
