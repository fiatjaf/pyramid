package grasp

import (
	"context"
	"os"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"github.com/stretchr/testify/require"

	"github.com/fiatjaf/pyramid/global"
)

// TestStatusRootKinds is the gate for appkit-y8g.10.
//
// NIP-34 ("Status", vendored/nips/34.md:199): "Root Patches, PRs and Issues have
// a Status". Before the fix, RejectIncomingEvent accepted 1630-1633 only when the
// e-tag root resolved to a stored 1617 or 1618, so a status against a kind:1621
// issue was refused and the §7.4 projection mapping (1621 + 1630-1632) was dead.
func TestStatusRootKinds(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "grasp_status_root_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	mmmm := &mmm.MultiMmapManager{Dir: tmpDir}
	require.NoError(t, mmmm.Init())
	defer mmmm.Close()

	il, err := mmmm.EnsureLayer("main")
	require.NoError(t, err)
	global.IL.Main = il

	sk := nostr.Generate()

	// store one root of each kind the spec says carries a Status, plus a kind:1
	// note that is not a valid root at all.
	store := func(kind nostr.Kind) nostr.ID {
		evt := nostr.Event{
			PubKey:    sk.Public(),
			CreatedAt: nostr.Now(),
			Kind:      kind,
			Tags:      nostr.Tags{nostr.Tag{"a", "30617:" + sk.Public().Hex() + ":testrepo"}},
			Content:   "root",
		}
		require.NoError(t, evt.Sign(sk))
		require.NoError(t, il.SaveEvent(evt))
		return evt.ID
	}

	issue := store(1621)
	patch := store(1617)
	pr := store(1618)
	note := store(1)

	// an id that was never stored
	missing := "0000000000000000000000000000000000000000000000000000000000000001"

	status := func(kind nostr.Kind, root string) nostr.Event {
		evt := nostr.Event{
			PubKey:    sk.Public(),
			CreatedAt: nostr.Now(),
			Kind:      kind,
			Tags:      nostr.Tags{nostr.Tag{"e", root, "", "root"}},
			Content:   "status",
		}
		require.NoError(t, evt.Sign(sk))
		return evt
	}

	for _, tc := range []struct {
		name       string
		kind       nostr.Kind
		root       string
		wantReject bool
	}{
		// the fix: every status kind on a stored 1621 issue root must pass
		{"1630 open on issue root", 1630, issue.Hex(), false},
		{"1631 resolved on issue root", 1631, issue.Hex(), false},
		{"1632 closed on issue root", 1632, issue.Hex(), false},
		{"1633 draft on issue root", 1633, issue.Hex(), false},

		// pre-existing behaviour must survive
		{"1630 on patch root", 1630, patch.Hex(), false},
		{"1630 on pull request root", 1630, pr.Hex(), false},

		// still refused
		{"1630 on nonexistent root", 1630, missing, true},
		{"1633 on nonexistent root", 1633, missing, true},
		{"1630 on kind:1 root", 1630, note.Hex(), true},
		{"1630 with no e tag", 1630, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evt := status(tc.kind, tc.root)
			if tc.root == "" {
				evt.Tags = nostr.Tags{}
				require.NoError(t, evt.Sign(sk))
			}
			reject, reason := RejectIncomingEvent(context.Background(), evt)
			require.Equal(t, tc.wantReject, reject, "reject=%v reason=%q", reject, reason)
			if !tc.wantReject {
				require.Empty(t, reason)
			}
		})
	}
}
