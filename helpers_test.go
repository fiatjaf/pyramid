package main

import (
	"strings"
	"testing"
)

func TestNormalizeDomainInput(t *testing.T) {
	// A 63-octet label, the longest DNS permits (RFC 1035 §2.3.4). Nostr
	// npubs are exactly this length, so any <npub>.<tld> name sits on the
	// boundary — which is how the off-by-one this test pins down was found.
	maxLabel := strings.Repeat("a", 63)
	npub := "npub1lsc2jmdhgwjaxdm3jgyv49wk8nyq29d0vps5ctuhfekk3d3vz8uq700rw4"

	for _, tc := range []struct {
		name  string
		in    string
		want  string
		valid bool
	}{
		{"simple", "relay.example.com", "relay.example.com", true},
		{"single label", "localhost", "localhost", true},
		{"strips https", "https://relay.example.com", "relay.example.com", true},
		{"strips wss and slash", "wss://relay.example.com/", "relay.example.com", true},
		{"keeps port", "relay.example.com:8080", "relay.example.com:8080", true},
		{"empty", "", "", true},
		{"punycode", "xn--bcher-kva.example", "xn--bcher-kva.example", true},

		{"62 octet label", strings.Repeat("a", 62) + ".example", strings.Repeat("a", 62) + ".example", true},
		{"63 octet label", maxLabel + ".example", maxLabel + ".example", true},
		{"63 octet final label", "example." + maxLabel, "example." + maxLabel, true},
		{"63 octet single label", maxLabel, maxLabel, true},
		{"npub label", npub + ".fips", npub + ".fips", true},

		{"64 octet label", strings.Repeat("a", 64) + ".example", "", false},
		{"uppercase", "Relay.example.com", "", false},
		{"space inside", "relay .example.com", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeDomainInput(tc.in)
			if tc.valid && err != nil {
				t.Fatalf("normalizeDomainInput(%q) returned error: %v", tc.in, err)
			}
			if !tc.valid {
				if err == nil {
					t.Fatalf("normalizeDomainInput(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("normalizeDomainInput(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
