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

	// collect the text to scan: content plus every tag value
	var sb strings.Builder
	sb.WriteString(event.Content)
	for _, tag := range event.Tags {
		for _, v := range tag {
			sb.WriteByte(' ')
			sb.WriteString(v)
		}
	}

	// match blob urls that point to this server: <domain>/<sha256>
	re := regexp.MustCompile(regexp.QuoteMeta(domain) + `/([0-9a-f]{64})`)

	seen := make(map[string]struct{})
	for _, m := range re.FindAllStringSubmatch(sb.String(), -1) {
		sha256 := m[1]
		if _, ok := seen[sha256]; ok {
			continue
		}
		seen[sha256] = struct{}{}

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
