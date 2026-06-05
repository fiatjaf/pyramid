package nsite

import (
	"fmt"
	"math/big"

	"fiatjaf.com/nostr"
)

func decodePubkeyB36(s string) (nostr.PubKey, error) {
	n, ok := new(big.Int).SetString(s, 36)
	if !ok {
		return nostr.ZeroPK, fmt.Errorf("invalid base36 pubkey")
	}
	b := n.Bytes()
	if len(b) > 32 {
		return nostr.ZeroPK, fmt.Errorf("base36 pubkey too large")
	}
	var pubkey nostr.PubKey
	copy(pubkey[32-len(b):], b)
	return pubkey, nil
}

func encodePubkeyB36(pubkey nostr.PubKey) string {
	n := new(big.Int).SetBytes(pubkey[:])
	return n.Text(36)
}
