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
	"github.com/puzpuzpuz/xsync/v3"
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

	queue := xsync.NewMapOf[nostr.PubKey, struct{}](xsync.WithPresize(len(members) * 200))
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

				queue.Store(f.Pubkey, struct{}{})
			}
		})
	}

	wg.Wait()

	res := make(chan nostr.PubKey)
	all := sync.WaitGroup{}

	log.Info().Int("n", queue.Size()).Msg("fetching secondary follow lists for follows")
	for user := range queue.Range {
		all.Add(1)
		go func() {
			if err := sem.Acquire(ctx, 1); err != nil {
				log.Error().Err(err).Msg("failed to acquire semaphore on wot building")
			}

			go func() {
				ctx, cancel := context.WithTimeout(ctx, time.Second*7)
				defer cancel()

				fl := global.Nostr.FetchFollowList(ctx, user).Items
				sem.Release(1)

				res <- user
				for _, f := range fl {
					if slices.Contains(global.Settings.Inbox.SpecificallyBlocked, f.Pubkey) {
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

	log.Info().Int("n", len(shids)).Msg("finishing wot xor filter")
	if len(shids) == 0 {
		return WotXorFilter{}
	}

	xf, err := xorfilter.Populate(shids)
	if err != nil {
		nostr.InfoLogger.Println("failed to populate filter", len(shids), err)
		return WotXorFilter{}
	}
	return WotXorFilter{len(shids), *xf}
}
