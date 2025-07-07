package main

import (
	sdk "fiatjaf.com/nostr/sdk"
)

var sys = (func() *sdk.System {
	sys := sdk.NewSystem()
	sys.Store = db
	return sys
})()
