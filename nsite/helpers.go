package nsite

import (
	"fmt"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip5a"
	"github.com/fiatjaf/pyramid/global"
)

func resolveSite(host string) (nostr.Event, error) {
	domain := strings.Trim(strings.ToLower(global.Settings.Nsite.Domain), ".")
	if host == domain {
		return nostr.Event{}, fmt.Errorf("domain mismatch")
	}

	label := strings.TrimSuffix(host, "."+domain)
	label = strings.TrimSuffix(label, ".")
	if label == "" || strings.Contains(label, ".") {
		return nostr.Event{}, fmt.Errorf("suffix mismatch")
	}

	pubkey, identifier, _, err := nip5a.DecodeSiteURL(label)
	if err != nil {
		return nostr.Event{}, fmt.Errorf("failed to decode nsite URL %s: %w", label, err)
	}

	filter := nostr.Filter{Authors: []nostr.PubKey{pubkey}}
	if identifier == "" {
		filter.Kinds = []nostr.Kind{15128}
	} else {
		filter.Kinds = []nostr.Kind{35128}
		filter.Tags = nostr.TagMap{"d": []string{identifier}}
	}

	var manifest nostr.Event
	for evt := range global.IL.Main.QueryEvents(filter, 10) {
		manifest = evt
	}

	if manifest.ID == nostr.ZeroID {
		return nostr.Event{}, fmt.Errorf("couldn't find nsite manifest")
	}

	return manifest, nil
}
