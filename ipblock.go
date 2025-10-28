package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"

	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip86"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

func ipBlockMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(global.Settings.BlockedIPs) > 0 {
			if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
				if slices.Contains(global.Settings.BlockedIPs, strings.Split(ip, ",")[0]) {
					http.Error(w, "IP blocked", 403)
					return
				}
			}
			if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
				if slices.Contains(global.Settings.BlockedIPs, ip) {
					http.Error(w, "IP blocked", 403)
					return
				}
			}
			if ip := r.RemoteAddr; ip != "" {
				if slices.Contains(global.Settings.BlockedIPs, ip) {
					http.Error(w, "IP blocked", 403)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func listBlockedIPsHandler(ctx context.Context) ([]nip86.IPReason, error) {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return nil, fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return nil, fmt.Errorf("unauthorized")
	}

	var res []nip86.IPReason
	for _, ip := range global.Settings.BlockedIPs {
		res = append(res, nip86.IPReason{IP: ip, Reason: ""})
	}
	return res, nil
}

func blockIPHandler(ctx context.Context, ip net.IP, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	if !slices.Contains(global.Settings.BlockedIPs, ip.String()) {
		global.Settings.BlockedIPs = append(global.Settings.BlockedIPs, ip.String())
	}
	return global.SaveUserSettings()
}

func unblockIPHandler(ctx context.Context, ip net.IP, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.BlockedIPs = slices.DeleteFunc(global.Settings.BlockedIPs, func(s string) bool { return s == ip.String() })
	return global.SaveUserSettings()
}
