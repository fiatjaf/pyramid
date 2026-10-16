package global

import (
	"context"
	"fmt"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"github.com/puzpuzpuz/xsync/v3"
)

type trackedConnection struct {
	subscriptions int
	cost          int
}

var subscriptionTracker = xsync.NewMapOf[string, trackedConnection]()

type debouncedRejectionLogger struct {
	mu               sync.Mutex
	count            int
	timer            *time.Timer
	client           string
	path             string
	currentOpen      int
	currentTotalCost int
}

var rejectionDebouncer = xsync.NewMapOf[string, *debouncedRejectionLogger]()

func logRejectionDebounced(ip, client, path string, currentOpen, currentTotalCost int) {
	rejectionDebouncer.Compute(ip, func(d *debouncedRejectionLogger, loaded bool) (*debouncedRejectionLogger, bool) {
		if !loaded {
			d = &debouncedRejectionLogger{}
		}
		d.mu.Lock()
		d.count++
		// keep the details of the latest rejection
		d.client = client
		d.path = path
		d.currentOpen = currentOpen
		d.currentTotalCost = currentTotalCost
		if d.timer == nil {
			d.timer = time.AfterFunc(5*time.Second, func() {
				d.mu.Lock()
				count := d.count
				client := d.client
				path := d.path
				currentOpen := d.currentOpen
				currentTotalCost := d.currentTotalCost
				d.count = 0
				d.timer = nil
				d.mu.Unlock()
				logEntry := Log.Info().
					Str("ip", ip).
					Str("client", client).
					Str("path", path).
					Int("rejections", count)

				if currentOpen > 0 {
					logEntry = logEntry.Int("open", currentOpen)
				}
				if currentTotalCost > 0 {
					logEntry = logEntry.Int("total-cost", currentTotalCost)
				}

				logEntry.
					Msg("rejected subscriptions")
			})
		}
		d.mu.Unlock()
		return d, false
	})
}

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
		if ws == nil || ws.Request == nil {
			return
		}

		ip := khatru.GetIPFromRequest(ws.Request)
		if ip == "" {
			return
		}

		subscriptionTracker.Compute(ip, func(v trackedConnection, loaded bool) (trackedConnection, bool) {
			v.subscriptions--
			v.cost -= GetFilterCost(filter)
			// drop the entry once this IP has no open subscriptions so the map
			// doesn't grow unboundedly with distinct client IPs
			if v.subscriptions <= 0 {
				return v, true
			}
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

	v, _ := subscriptionTracker.Load(ip)

	var currentOpen int
	var currentTotalCost int
	if v.subscriptions >= Settings.Limits.MaxSubscriptionsOpen {
		currentOpen = v.subscriptions
	} else if v.cost >= Settings.Limits.MaxTotalCostOpen {
		currentTotalCost = v.cost
	} else {
		return false, ""
	}

	var client, path string
	if conn := khatru.GetConnection(ctx); conn != nil && conn.Request != nil {
		client = conn.Request.Header.Get("Origin")
		if client == "" {
			client = conn.Request.Header.Get("user-agent")
		}
		path = conn.Request.Host + conn.Request.URL.Path
	}

	logRejectionDebounced(ip, client, path, currentOpen, currentTotalCost)

	if v.subscriptions >= Settings.Limits.MaxSubscriptionsOpen {
		return true, fmt.Sprintf("already %d subscriptions from this IP", v.subscriptions)
	}
	return true, fmt.Sprintf("there are subscriptions from this IP with a total filter cost of %d", v.cost)
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
