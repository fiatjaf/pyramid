package moderated

import (
	"net/http"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

func approveHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsMember(loggedUser) {
		http.Error(w, "unauthorized: must be a member", 403)
		return
	}

	id, err := nostr.IDFromHex(r.PathValue("eventId"))
	if err != nil {
		http.Error(w, "invalid event id", 400)
		return
	}

	// get event from queue
	var evt nostr.Event
	var found bool
	for e := range global.IL.ModerationQueue.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
		evt = e
		found = true
		break
	}
	if !found {
		http.Error(w, "event not found in queue", 404)
		return
	}

	// save to moderated layer
	if evt.Kind.IsAddressable() || evt.Kind.IsReplaceable() {
		err = global.IL.Moderated.ReplaceEvent(evt)
	} else {
		err = global.IL.Moderated.SaveEvent(evt)
	}
	if err != nil {
		http.Error(w, "failed to approve event: "+err.Error(), 500)
		return
	}

	// delete from queue
	if err := global.IL.ModerationQueue.DeleteEvent(evt.ID); err != nil {
		log.Error().Err(err).Str("id", evt.ID.String()).Msg("failed to delete from queue after approval")
	}

	log.Info().Str("id", evt.ID.Hex()).Str("approver", loggedUser.Hex()).Msg("event approved")
	Relay.BroadcastEvent(evt)
	http.Redirect(w, r, "/moderated/", 302)
}

func rejectHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsMember(loggedUser) {
		http.Error(w, "unauthorized: must be a member", 403)
		return
	}

	id, err := nostr.IDFromHex(r.PathValue("eventId"))
	if err != nil {
		http.Error(w, "invalid event id", 400)
		return
	}

	// delete from queue
	if err := global.IL.ModerationQueue.DeleteEvent(id); err != nil {
		http.Error(w, "failed to reject event: "+err.Error(), 500)
		return
	}

	log.Info().Str("id", id.Hex()).Str("rejector", loggedUser.Hex()).Msg("event rejected")
	http.Redirect(w, r, "/moderated/", 302)
}
