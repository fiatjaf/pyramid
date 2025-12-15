package inbox

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
	"golang.org/x/sync/semaphore"
)

func pubKeyToShid(pubkey nostr.PubKey) uint64 {
	return binary.BigEndian.Uint64(pubkey[16:24])
}

type WotXorFilter struct {
	Items int
	xorfilter.Xor8
}

func (wxf WotXorFilter) Contains(pubkey nostr.PubKey) bool {
	if wxf.Items == 0 {
		return false
	}
	return wxf.Xor8.Contains(pubKeyToShid(pubkey))
}

func computeAggregatedWoT(ctx context.Context) (WotXorFilter, error) {
	members := make([]nostr.PubKey, 0, pyramid.Members.Size())
	for k := range pyramid.Members.Range {
		members = append(members, k)
	}

	queue := make(map[nostr.PubKey]struct{}, len(members)*100)
	wg := sync.WaitGroup{}
	sem := semaphore.NewWeighted(15)

	log.Info().Int("n", len(members)).Msg("fetching primary follow lists for members")
	for _, member := range members {
		if err := sem.Acquire(ctx, 1); err != nil {
			return WotXorFilter{}, fmt.Errorf("failed to acquire: %w", err)
		}

		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, time.Second*7)
			defer cancel()
			defer sem.Release(1)

			for _, f := range global.Nostr.FetchFollowList(ctx, member).Items {
				if slices.Contains(global.Settings.Inbox.SpecificallyBlocked, f.Pubkey) {
					continue
				}

				queue[f.Pubkey] = struct{}{}
			}
		})
	}

	wg.Wait()

	res := make(chan nostr.PubKey)

	log.Info().Int("n", len(queue)).Msg("fetching secondary follow lists for follows")
	for user := range queue {
		if err := sem.Acquire(ctx, 1); err != nil {
			return WotXorFilter{}, fmt.Errorf("failed to acquire: %w", err)
		}

		go func() {
			ctx, cancel := context.WithTimeout(ctx, time.Second*7)
			defer cancel()
			defer sem.Release(1)

			res <- user
			for _, f := range global.Nostr.FetchFollowList(ctx, user).Items {
				if slices.Contains(global.Settings.Inbox.SpecificallyBlocked, f.Pubkey) {
					continue
				}

				res <- f.Pubkey
			}
		}()
	}

	return makeWoTFilter(res), nil
}

func makeWoTFilter(m chan nostr.PubKey) WotXorFilter {
	shids := make([]uint64, 0, 60000)
	shidMap := make(map[uint64]struct{}, 60000)
	for pk := range m {
		shid := pubKeyToShid(pk)
		if _, alreadyAdded := shidMap[shid]; !alreadyAdded {
			shidMap[shid] = struct{}{}
			shids = append(shids, shid)
		}
	}

	if len(shids) == 0 {
		return WotXorFilter{}
	}

	log.Info().Int("n", len(shids)).Msg("finishing wot xor filter")
	xf, err := xorfilter.Populate(shids)
	if err != nil {
		nostr.InfoLogger.Println("failed to populate filter", len(shids), err)
		return WotXorFilter{}
	}
	return WotXorFilter{len(shids), *xf}
}
