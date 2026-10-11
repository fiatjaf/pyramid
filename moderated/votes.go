package moderated

import (
	"encoding/json"
	"strconv"
	"strings"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

const approvalTarget = 100

const rejectionTarget = 100

func GetApprovalTarget() int {
	return approvalTarget
}

type voteSet map[nostr.PubKey]int

func votesKey(id nostr.ID) []byte {
	return []byte("moderated:votes:" + id.Hex())
}

func rejectionVotesKey(id nostr.ID) []byte {
	return []byte("moderated:rejection-votes:" + id.Hex())
}

func loadVotes(key []byte) voteSet {
	votes := make(voteSet, 1)
	if global.Nostr == nil || global.Nostr.KVStore == nil {
		return votes
	}
	data, err := global.Nostr.KVStore.Get(key)
	if err != nil || data == nil {
		return votes
	}
	json.Unmarshal(data, &votes)
	return votes
}

func saveVotes(key []byte, votes voteSet) {
	data, err := json.Marshal(votes)
	if err != nil {
		return
	}
	global.Nostr.KVStore.Set(key, data)
}

func deleteVotes(id nostr.ID) {
	global.Nostr.KVStore.Delete(votesKey(id))
	global.Nostr.KVStore.Delete(rejectionVotesKey(id))
}

func (votes voteSet) total() int {
	total := 0
	for _, worth := range votes {
		total += worth
	}
	return total
}

// GetVoteWorth returns how many points a single vote from the given member
// is worth, based on the configured approval_votes_spec:
//   - empty or a single number: every vote is worth that number (default 100)
//   - slash-separated pattern like "50/25/13": each pyramid level has its
//     own worth (level 1 is worth 50, level 2 is worth 25, ...), while
//     root members (level 0) are always worth 100.
func GetVoteWorth(approver nostr.PubKey) int {
	spec := global.Settings.Moderated.ApprovalVotesSpec
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return approvalTarget
	}

	if pyramid.IsRoot(approver) || pyramid.GetLevel(approver) == 0 {
		return approvalTarget
	}

	parts := strings.Split(spec, "/")
	if len(parts) == 1 {
		if n, err := strconv.Atoi(parts[0]); err == nil && n > 0 {
			return n
		}
	}

	// per-level worth
	level := pyramid.GetLevel(approver)
	if level < 1 {
		// non-members and roots don't get here
		return 0
	}
	idx := level - 1
	levels := strings.Split(spec, "/")
	if idx >= len(levels) {
		idx = len(levels) - 1
	}
	if n, err := strconv.Atoi(levels[idx]); err == nil && n > 0 {
		return n
	}

	// invalid configuration falls back to the default
	return approvalTarget
}

func GetVoteTotal(id nostr.ID) int {
	return loadVotes(votesKey(id)).total()
}

func vote(approver nostr.PubKey, id nostr.ID) bool {
	return voteWithTarget(approver, votesKey(id), approvalTarget)
}

func rejectionVote(rejector nostr.PubKey, evt nostr.Event) bool {
	return voteWithTarget(rejector, rejectionVotesKey(evt.ID), GetRejectionTarget(evt))
}

func GetRejectionVoteTotal(id nostr.ID) int {
	return loadVotes(rejectionVotesKey(id)).total()
}

func GetRejectionTarget(evt nostr.Event) int {
	days := int((nostr.Now() - evt.CreatedAt) / (24 * 60 * 60))
	return rejectionTarget - days
}

// voteWithTarget records the voter's vote (if they haven't voted yet) and
// reports whether the total has reached the target
func voteWithTarget(voter nostr.PubKey, key []byte, target int) bool {
	votes := loadVotes(key)

	if _, ok := votes[voter]; !ok {
		votes[voter] = GetVoteWorth(voter)
		saveVotes(key, votes)
	}

	return votes.total() >= target
}
