package grasp

import (
	"context"
	"strings"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
)

func RejectIncomingEvent(ctx context.Context, event nostr.Event) (reject bool, reason string) {
	// validate repository-related events
	switch event.Kind {
	case 30618, 1621, 1617, 1618:
		// these kinds must reference an existing repository announcement (kind 30617)
		if aTag := event.Tags.Find("a"); aTag == nil || !repositoryExists(aTag[1]) {
			return true, "repository not found: must reference an existing repository announcement (kind 30617)"
		}
	case 1619:
		// kind 1619 must reference an existing kind 1618
		if eTag := event.Tags.Find("E"); eTag == nil || !refExistsAsKind(eTag[1], []nostr.Kind{1618}) {
			return true, "pull request not found: must reference an existing pull request (kind 1618)"
		}
	case 1630, 1631, 1632, 1633:
		// these kinds must reference an existing kind 1617 or 1618
		if eTag := event.Tags.Find("e"); eTag == nil || !refExistsAsKind(eTag[1], []nostr.Kind{1617, 1618}) {
			return true, "issue not found: must reference an existing issue or pull request (kind 1617 or 1618)"
		}
	}

	return false, ""
}

func repositoryExists(aTag string) bool {
	// aTag format: 30617:<pubkey>:<identifier>
	if aTag == "" {
		return false
	}

	parts := strings.Split(aTag, ":")
	if len(parts) != 3 || parts[0] != "30617" {
		return false
	}

	pubKey, err := nostr.PubKeyFromHex(parts[1])
	if err != nil {
		return false
	}

	// Check if we have a repository announcement (kind 30617) with this identifier
	for range global.IL.Main.QueryEvents(nostr.Filter{
		Kinds:   []nostr.Kind{30617},
		Authors: []nostr.PubKey{pubKey},
		Tags: nostr.TagMap{
			"d": []string{parts[2]},
		},
	}, 1) {
		return true
	}

	return false
}

func refExistsAsKind(eventId string, kinds []nostr.Kind) bool {
	if eventId == "" || len(kinds) == 0 {
		return false
	}

	id, err := nostr.IDFromHex(eventId)
	if err != nil {
		return false
	}

	for range global.IL.Main.QueryEvents(nostr.Filter{
		IDs:   []nostr.ID{id},
		Kinds: kinds,
	}, 1) {
		return true
	}

	return false
}
