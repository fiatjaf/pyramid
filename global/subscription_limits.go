package global

import (
	"context"
	"fmt"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"github.com/puzpuzpuz/xsync/v3"
)

type trackedConnection struct {
	subscriptions int
	cost          int
}

var subscriptionTracker = xsync.NewMapOf[string, trackedConnection]()

func NewRelay() *khatru.Relay {
	relay := khatru.NewRelay()

	relay.OnListenerAdded = func(ws *khatru.WebSocket, ssid int, id string, filter nostr.Filter) {
		if ws == nil || ws.Request == nil {
			return
		}

		ip := khatru.GetIPFromRequest(ws.Request)
		if ip == "" {
			return
		}

		subscriptionTracker.Compute(ip, func(v trackedConnection, loaded bool) (trackedConnection, bool) {
			v.subscriptions++
			v.cost += GetFilterCost(filter)
			return v, false
		})
	}

	relay.OnListenerRemoved = func(ws *khatru.WebSocket, ssid int, id string, filter nostr.Filter) {
		if ws == nil {
			return
		}

		ip := khatru.GetIPFromRequest(ws.Request)
		if ip == "" {
			return
		}

		subscriptionTracker.Compute(ip, func(v trackedConnection, loaded bool) (trackedConnection, bool) {
			v.subscriptions--
			v.cost -= GetFilterCost(filter)
			return v, false
		})
	}

	return relay
}

func RejectTooManyOpenSubscriptions(ctx context.Context, _ nostr.Filter) (bool, string) {
	ip := khatru.GetIP(ctx)
	if ip == "" {
		return false, ""
	}

	if v, _ := subscriptionTracker.Load(ip); v.subscriptions >= Settings.Limits.MaxSubscriptionsOpen {
		Log.Info().Str("ip", ip).Int("subs", v.subscriptions).Msg("rejected subscription due to max number of subscriptions reached")
		return true, fmt.Sprintf("already %d subscriptions from this IP", v.subscriptions)
	} else if v.cost >= Settings.Limits.MaxTotalCostOpen {
		Log.Info().Str("ip", ip).Int("cost", v.cost).Msg("rejected subscription due to max cost reached")
		return true, fmt.Sprintf("there are subscriptions from this IP with a total filter cost of %d", v.cost)
	}

	return false, ""
}

//go:inline
func GetFilterCost(filter nostr.Filter) int {
	if filter.Authors != nil {
		return len(filter.Authors)
	}

	if filter.Kinds != nil {
		return len(filter.Kinds)
	}

	if filter.Tags != nil {
		sum := 0
		for _, tagv := range filter.Tags {
			sum += len(tagv)
		}
		return sum
	}

	return 1
}
