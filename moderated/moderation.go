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

	// cast (or fetch) a vote for this event
	if !vote(approver, id) {
		log.Info().Str("id", id.Hex()).Str("voter", approver.Hex()).Int("points", GetVoteTotal(id)).
			Msg("approval vote cast, not yet enough to approve")
		return nil
	}

	// save to moderated layer
	var saveErr error
	if evt.Kind.IsAddressable() || evt.Kind.IsReplaceable() {
		_, saveErr = global.IL.Moderated.ReplaceEvent(evt)
	} else {
		saveErr = global.IL.Moderated.SaveEvent(evt)
	}
	if saveErr != nil {
		return saveErr
	}

	// delete from queue
	if err := global.IL.ModerationQueue.DeleteEvent(evt.ID); err != nil {
		log.Error().Err(err).Str("id", evt.ID.String()).Msg("failed to delete from queue after approval")
	}

	// forget the votes along with the event
	deleteVotes(id)

	// broadcast to listeners
	count := Relay.ForceBroadcastEvent(evt)
	log.Info().Str("id", evt.ID.Hex()).Str("approver", approver.Hex()).Int("broadcasted", count).
		Msg("event approved and broadcasted")

	return nil
}

func rejectEvent(rejector nostr.PubKey, id nostr.ID) error {
	var evt nostr.Event
	found := false
	for e := range global.IL.ModerationQueue.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
		evt = e
		found = true
		break
	}
	if !found {
		return fmt.Errorf("event not found in queue")
	}

	// the rejection target decays as the event ages, so it may be reached
	// even if this vote doesn't add any points (e.g. a repeated vote)
	if !rejectionVote(rejector, evt) {
		log.Info().Str("id", id.Hex()).Str("voter", rejector.Hex()).Int("points", GetRejectionVoteTotal(id)).
			Msg("rejection vote cast, not yet enough to reject")
		return nil
	}

	if err := global.IL.ModerationQueue.DeleteEvent(id); err != nil {
		return err
	}

	// forget any votes cast for this event
	deleteVotes(id)

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

	http.Redirect(w, r, global.Settings.Moderated.GetPageURL(), 302)
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

	http.Redirect(w, r, global.Settings.Moderated.GetPageURL(), 302)
}

func approveEventsHandler(w http.ResponseWriter, r *http.Request) {
	moderateEventsHandler(w, r, approveEvent)
}

func rejectEventsHandler(w http.ResponseWriter, r *http.Request) {
	moderateEventsHandler(w, r, rejectEvent)
}

func moderateEventsHandler(w http.ResponseWriter, r *http.Request, moderate func(nostr.PubKey, nostr.ID) error) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsMember(loggedUser) {
		http.Error(w, "unauthorized: must be a member", 403)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid event ids", 400)
		return
	}
	for _, rawID := range r.Form["selectedEventId"] {
		id, err := nostr.IDFromHex(rawID)
		if err != nil {
			http.Error(w, "invalid event id", 400)
			return
		}
		if err := moderate(loggedUser, id); err != nil {
			http.Error(w, "failed to moderate event: "+err.Error(), 500)
			return
		}
	}

	http.Redirect(w, r, global.Settings.Moderated.GetPageURL(), 302)
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
	for evt := range global.IL.ModerationQueue.QueryEvents(nostr.Filter{}, 1_000) {
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
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	// root users can remove events that were already approved
	if pyramid.IsRoot(caller) {
		for range global.IL.Moderated.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
			if err := global.IL.Moderated.DeleteEvent(id); err != nil {
				return err
			}
			log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("approved event removed by root")
			return nil
		}
	}

	// allow if caller is a member (includes root users)
	if pyramid.IsMember(caller) {
		log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("moderated banevent called by member")
	} else {
		// check if the caller is the author of the event being banned
		var isAuthor bool
		for evt := range global.IL.ModerationQueue.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
			if evt.PubKey == caller {
				isAuthor = true
				break
			}
		}
		if !isAuthor {
			return fmt.Errorf("must be a member or the event author to ban an event")
		}
		log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("moderated banevent called by author")

		// authors can always retract their own pending events, no vote needed
		if err := global.IL.ModerationQueue.DeleteEvent(id); err != nil {
			return err
		}
		deleteVotes(id)
		return nil
	}

	return rejectEvent(caller, id)
}
