# Using pyramid as a git / forge relay (gittr)

Upstream [pyramid](https://github.com/fiatjaf/pyramid) already understands most **NIP-34** kinds and optional **GRASP**. This fork adds:

1. **Extra default kinds** used by gittr / gitnostr: `50`, `51`, `52` (SSH keys), `10011` (NIP-39 identities), `10018`, `9806` (bounties), `1337`, `1624`, plus a few app/page kinds.
2. **`open_kinds_spec`** — kinds that **anyone** (not only pyramid members) may publish. Empty = classic pyramid (members-only writes).

## Why

Git-oriented relays often reject bare kind **52** / other forge events (“must reference an accepted repository”). A forge needs one relay that:

- accepts the full small set of git + identity + SSH kinds
- stays readable for clients without membership
- still keeps kind-1 chat spam off the open path via membership

## Recommended settings (UI → general)

**Allowed kinds:** leave default, or `all`, or `+52,+10011,+10018,+9806,+1337,+1624,+50,+51` if you prefer deltas on an older data dir.

**Open kinds (anyone may publish):**

```
0,3,5,7,50,51,52,1111,1337,1617,1618,1619,1621,1624,1630,1631,1632,1633,1985,9735,9806,10011,10018,10317,30023,30617,30618,15128,35128
```

Membership still gates notes / social; open kinds cover profiles, follows, SSH keys, NIP-34, zaps, bounties, identities.

## GRASP

Optional. When enabled, patch/issue events must reference a **30617** already on **this** relay (same idea as other git relays). For a pure event-propagation relay, leave GRASP off and keep git hosting on gittr’s bridge (`git.gittr.space`). For a combined GRASP+relay host, enable GRASP and open-write `30617` first.

## Point gittr at it

Add `wss://relay.yourdomain` to default / user relays and to Settings → SSH Keys fallback relays so kind-52 lists don’t depend on damus alone.
