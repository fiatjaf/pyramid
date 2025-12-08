# Test Infrastructure

this package provides test infrastructure for the pyramid relay.

## overview

the test setup starts a relay instance and creates multiple test users with different keypairs, some members and some non-members.

## usage

```go
func TestYourFeature(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // start relay and create users
    err := Setup(ctx)
    require.NoError(t, err)

    // access test users
    member := Users[0]      // first 5 users (0-4) are members
    nonMember := Users[5]   // last 5 users (5-9) are non-members

    // create and sign events
    event := nostr.Event{
        Kind:      1,
        PubKey:    member.PubKey,
        CreatedAt: nostr.Now(),
        Content:   "test content",
        Tags:      nostr.Tags{},
    }
    event.Sign(member.PrivateKey)

    // save to relay stores
    global.IL.Main.SaveEvent(event)

    // query events
    events := global.IL.Main.QueryEvents(nostr.Filter{
        Authors: []nostr.PubKey{member.PubKey},
    }, 10)
}
```

## available test users

- `Users[0-4]`: members of the relay (IsMember = true)
- `Users[5-9]`: non-members (IsMember = false)

each user has:
- `PrivateKey`: [32]byte for signing events
- `PubKey`: nostr.PubKey for the user
- `IsMember`: bool indicating membership status

## relay urls

access different relay endpoints via `RelayURLs`:

- `RelayURLs[0]`: main relay
- `RelayURLs[1]`: internal relay
- `RelayURLs[2]`: favorites relay
- `RelayURLs[3]`: uppermost relay
- `RelayURLs[4]`: popular relay
- `RelayURLs[5]`: groups relay
- `RelayURLs[6]`: inbox relay

## cleanup

the relay and resources are automatically cleaned up when the test context is cancelled.
