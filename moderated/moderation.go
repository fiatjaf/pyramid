package moderated

import (
	"context"
	"fmt"
	"net/http"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip86"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

func approveEvent(approver nostr.PubKey, id nostr.ID) error {
	// get event from queue
	var evt nostr.Event
	var found bool
	for e := range global.IL.ModerationQueue.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
		evt = e
		found = true
		break
	}
	if !found {
		return fmt.Errorf("event not found in queue")
	}

	// save to moderated layer
	var err error
	if evt.Kind.IsAddressable() || evt.Kind.IsReplaceable() {
		err = global.IL.Moderated.ReplaceEvent(evt)
	} else {
		err = global.IL.Moderated.SaveEvent(evt)
	}
	if err != nil {
		return err
	}

	// delete from queue
	if err := global.IL.ModerationQueue.DeleteEvent(evt.ID); err != nil {
		log.Error().Err(err).Str("id", evt.ID.String()).Msg("failed to delete from queue after approval")
	}

	log.Info().Str("id", evt.ID.Hex()).Str("approver", approver.Hex()).Msg("event approved")
	Relay.BroadcastEvent(evt)
	return nil
}

func rejectEvent(rejector nostr.PubKey, id nostr.ID) error {
	// delete from queue
	if err := global.IL.ModerationQueue.DeleteEvent(id); err != nil {
		return err
	}

	log.Info().Str("id", id.Hex()).Str("rejector", rejector.Hex()).Msg("event rejected")
	return nil
}

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

	if err := approveEvent(loggedUser, id); err != nil {
		http.Error(w, "failed to approve event: "+err.Error(), 500)
		return
	}

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

	if err := rejectEvent(loggedUser, id); err != nil {
		http.Error(w, "failed to reject event: "+err.Error(), 500)
		return
	}

	http.Redirect(w, r, "/moderated/", 302)
}

func listEventsNeedingModerationHandler(ctx context.Context) ([]nip86.IDReason, error) {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return nil, fmt.Errorf("not authenticated")
	}

	if !pyramid.IsMember(author) {
		return nil, fmt.Errorf("unauthorized")
	}

	var events []nip86.IDReason
	for evt := range global.IL.ModerationQueue.QueryEvents(nostr.Filter{}, 1000) {
		events = append(events, nip86.IDReason{ID: evt.ID})
	}
	return events, nil
}

func allowEventHandler(ctx context.Context, id nostr.ID, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsMember(author) {
		return fmt.Errorf("unauthorized")
	}

	return approveEvent(author, id)
}

func banEventHandler(ctx context.Context, id nostr.ID, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsMember(author) {
		return fmt.Errorf("unauthorized")
	}

	return rejectEvent(author, id)
}
