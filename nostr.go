package main

import (
	sdk "fiatjaf.com/nostr/sdk"
)

var sys = sdk.NewSystem(
	sdk.WithStore(db),
)
