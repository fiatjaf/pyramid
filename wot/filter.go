package wot

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"github.com/FastFilter/xorfilter"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/puzpuzpuz/xsync/v3"
	"golang.org/x/sync/semaphore"
)

var log = global.Log.With().Str("module", "wot").Logger()

// RejectAuthor is the shared rejection policy for events written to
// relays whose write access is gated by the web-of-trust (inbox, network,
// moderated): the author must not be specifically blocked and must be a
// relay member or inside the aggregated web-of-trust.
func RejectAuthor(pk nostr.PubKey) (reject bool, msg string) {
	if slices.Contains(global.Settings.Wot.SpecificallyBlocked, pk) {
		return true, "blocked: you are blocked"
	}

	if pyramid.IsMember(pk) {
		return false, ""
	}

	if !IsComputed() {
		return true, "blocked: wot still being computed, wait some minutes"
	}

	if !Contains(pk) {
		return true, "blocked: you're not in the extended network of this relay"
	}

	return false, ""
}

type XorFilter struct {
	Items int
	xorfilter.Xor8
}

func (f XorFilter) Contains(pubkey nostr.PubKey) bool {
	if f.Items == 0 {
		return false
	}
	return f.Xor8.Contains(pubKeyToShid(pubkey))
}

func pubKeyToShid(pubkey nostr.PubKey) uint64 {
	return binary.BigEndian.Uint64(pubkey[16:24])
}

// ComputeAggregated computes the aggregated web-of-trust filter for all pyramid
// members: it fetches the follow lists of all members, then the follow lists of
// all those followed pubkeys, and builds a xor filter from the full set.
func ComputeAggregated(ctx context.Context) (XorFilter, error) {
	members := make([]nostr.PubKey, 0, pyramid.Members.Size())
	for k := range pyramid.Members.Range {
		members = append(members, k)
	}

	queue := xsync.NewMapOf[nostr.PubKey, struct{}](xsync.WithPresize(len(members) * 200))
	followedBy := make(map[nostr.PubKey]int)
	var followedByMu sync.Mutex
	wg := sync.WaitGroup{}
	sem := semaphore.NewWeighted(15)

	log.Info().Int("n", len(members)).Msg("fetching primary follow lists for members")
	for _, member := range members {
		if slices.Contains(global.Settings.Wot.SpecificallyBlocked, member) {
			continue
		}

		if err := sem.Acquire(ctx, 1); err != nil {
			return XorFilter{}, fmt.Errorf("failed to acquire: %w", err)
		}

		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, time.Second*7)
			defer cancel()
			defer sem.Release(1)

			seen := make(map[nostr.PubKey]struct{})
			for _, f := range global.Nostr.FetchFollowList(ctx, member).Items {
				if slices.Contains(global.Settings.Wot.SpecificallyBlocked, f.Pubkey) {
					continue
				}

				if _, ok := seen[f.Pubkey]; ok {
					continue
				}
				seen[f.Pubkey] = struct{}{}
				followedByMu.Lock()
				followedBy[f.Pubkey]++
				followedByMu.Unlock()
				queue.Store(f.Pubkey, struct{}{})
			}
		})
	}

	wg.Wait()
	minFollowedBy := global.Settings.Wot.MinFollowedBy.Get(len(members))

	res := make(chan nostr.PubKey)
	all := sync.WaitGroup{}

	log.Info().Int("n", queue.Size()).Msg("fetching secondary follow lists for follows")
	for user := range queue.Range {
		if slices.Contains(global.Settings.Wot.SpecificallyBlocked, user) {
			continue
		}
		includeFollows := followedBy[user] >= minFollowedBy

		all.Add(1)
		go func() {
			if err := sem.Acquire(ctx, 1); err != nil {
				log.Error().Err(err).Msg("failed to acquire semaphore on wot building")
				all.Done()
				return
			}

			go func() {
				ctx, cancel := context.WithTimeout(ctx, time.Second*7)
				defer cancel()
				if !includeFollows {
					res <- user
					sem.Release(1)
					all.Done()
					return
				}

				fl := global.Nostr.FetchFollowList(ctx, user).Items
				sem.Release(1)

				res <- user
				for _, f := range fl {
					if slices.Contains(global.Settings.Wot.SpecificallyBlocked, f.Pubkey) {
						continue
					}
					res <- f.Pubkey
				}
				all.Done()
			}()
		}()
	}

	go func() {
		all.Wait()
		close(res)
	}()

	return makeFilter(res), nil
}

func makeFilter(m chan nostr.PubKey) XorFilter {
	shids := make([]uint64, 0, 60000)
	shidMap := make(map[uint64]struct{}, 60000)
	for pk := range m {
		if slices.Contains(global.Settings.Wot.SpecificallyBlocked, pk) {
			continue
		}

		shid := pubKeyToShid(pk)
		if _, alreadyAdded := shidMap[shid]; !alreadyAdded {
			shidMap[shid] = struct{}{}
			shids = append(shids, shid)
		}
	}

	log.Info().Int("n", len(shids)).Msg("finishing wot xor filter")
	if len(shids) == 0 {
		return XorFilter{}
	}

	xf, err := xorfilter.Populate(shids)
	if err != nil {
		nostr.InfoLogger.Println("failed to populate filter", len(shids), err)
		return XorFilter{}
	}
	return XorFilter{len(shids), *xf}
}
