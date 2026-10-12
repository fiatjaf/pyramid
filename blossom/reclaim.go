package blossom

import (
	"context"
	"regexp"
	"strings"

	"fiatjaf.com/nostr"

	"github.com/fiatjaf/pyramid/global"
)

// reclaimBlobsFromEvent scans an event that is about to be deleted for blobs
// hosted on this server and releases the storage owned by the event's author.
// The author is the uploader pyramid knows about, so this works whether the
// deletion was triggered by the author (kind 5) or by a group admin (kind 9005).
func reclaimBlobsFromEvent(ctx context.Context, id nostr.ID) {
	if !global.Settings.Blossom.ReclaimOnEventDeletion {
		return
	}

	domain := global.Settings.Domain
	if domain == "" {
		return
	}

	// fetch the event before it is removed so we can inspect its references
	var event nostr.Event
	found := false
	for evt := range global.IL.Main.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
		event = evt
		found = true
		break
	}
	if !found {
		return
	}

	// match blob urls that point to this server: <domain>/<sha256>
	re := regexp.MustCompile(regexp.QuoteMeta(domain) + `/([0-9a-f]{64})`)

	candidates := extractBlobs(re, event)
	if len(candidates) == 0 {
		return
	}

	// a blob stays if any other event by the same author still references it;
	// ownership events (24242) live in the blossom layer, so scanning main by
	// author only sees the author's real content events
	for evt := range global.IL.Main.QueryEvents(nostr.Filter{Authors: []nostr.PubKey{event.PubKey}}, 100_000) {
		if evt.ID == id {
			continue
		}
		for sha256 := range extractBlobs(re, evt) {
			delete(candidates, sha256)
		}
	}

	for sha256 := range candidates {
		// remove the author's ownership link for this blob
		if err := BlobIndex.Delete(ctx, sha256, event.PubKey); err != nil {
			log.Warn().Err(err).Str("sha256", sha256).Msg("failed to release blob ownership on event deletion")
			continue
		}

		// physically remove the file only if nobody else owns it
		if bd, _ := BlobIndex.Get(ctx, sha256); bd == nil {
			if err := deleteBlob(ctx, sha256, ""); err != nil {
				log.Warn().Err(err).Str("sha256", sha256).Msg("failed to delete orphaned blob file")
			}
		}
	}
}

// extractBlobs returns the set of blob sha256s referenced by an event, found in
// its content or any tag value as a url pointing to this server.
func extractBlobs(re *regexp.Regexp, event nostr.Event) map[string]struct{} {
	var sb strings.Builder
	sb.WriteString(event.Content)
	for _, tag := range event.Tags {
		for _, v := range tag {
			sb.WriteByte(' ')
			sb.WriteString(v)
		}
	}

	blobs := make(map[string]struct{})
	for _, m := range re.FindAllStringSubmatch(sb.String(), -1) {
		blobs[m[1]] = struct{}{}
	}
	return blobs
}
