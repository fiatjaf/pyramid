package global

import (
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip57"
	"fiatjaf.com/nostr/nip61"
	"github.com/puzpuzpuz/xsync/v3"
)

var canReadCache = xsync.NewMapOf[[16]byte, bool]()

// returns a cached boolean or computes it and returns
func CanReadPaywalled(author, reader nostr.PubKey) bool {
	if author == reader {
		return true
	}

	key := [16]byte{}
	copy(key[0:8], author[0:8])
	copy(key[8:16], reader[0:8])

	canRead, _ := canReadCache.LoadOrCompute(key, func() bool {
		sum := sumOfPayments(reader, author, Settings.Paywall.PeriodDays)
		return sum >= uint64(Settings.Paywall.AmountSats)
	})

	return canRead
}

func ResetPaywallCache(receiver, payer nostr.PubKey) {
	key := [16]byte{}
	copy(key[0:8], receiver[0:8])
	copy(key[8:16], payer[0:8])
	canReadCache.Delete(key)
}

// every time the day changes (on UTC) delete the entire cache
func paywallCacheCleanup() {
	for {
		now := time.Now().UTC()
		nextMidnight := now.AddDate(0, 0, 1).Truncate(24 * time.Hour)
		time.Sleep(time.Until(nextMidnight))
		canReadCache.Clear()
	}
}

// sumOfPayments returns an amount in sats
func sumOfPayments(from nostr.PubKey, to nostr.PubKey, lastDays uint) uint64 {
	// compute "since" as the timestamp where when the current day started minus lastDay
	since := nostr.Timestamp(time.Now().UTC().Truncate(24*time.Hour).Unix() - int64(lastDays)*24*3600)

	var total uint64 = 0

	for zap := range IL.Inbox.QueryEvents(nostr.Filter{
		Kinds: []nostr.Kind{9735},
		Tags:  nostr.TagMap{"P": []string{from.Hex()}, "p": []string{to.Hex()}},
		Since: since,
	}, 500) {
		total += nip57.GetAmountFromZap(zap)
	}

	// since zaps are measured in millisatoshis divide by 1000 here
	total /= 1000

	for nutzap := range IL.Inbox.QueryEvents(nostr.Filter{
		Kinds:   []nostr.Kind{9321},
		Authors: []nostr.PubKey{from},
		Tags:    nostr.TagMap{"p": []string{to.Hex()}},
		Since:   since,
	}, 500) {
		total += nip61.GetAmountFromNutzap(nutzap) // assume this returns an amount in sats
	}

	return total
}
