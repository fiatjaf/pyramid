# Forge adaptations (gittr / Nostr git)

**This repository is an adapted [fiatjaf/pyramid](https://github.com/fiatjaf/pyramid).**  
It is maintained for **Nostr git** workflows used by **[gittr](https://gittr.space)** and related clients (ngit, gitnostr bridge, etc.) — not as a drop-in replacement claim for every stock Pyramid deploy.

| | |
|--|--|
| Upstream | Community membership relay (hierarchy, subrelays, optional GRASP) |
| This fork | Same core + **gittr-oriented kind defaults** + **`open_kinds_spec`** so non-members can publish forge events |
| Repo name | Kept as `pyramid` so upstream tracking stays simple; branding is in README / GitHub About |

## Why adapt

Many git-oriented relays reject bare forge events (especially kind **52** SSH keys: “must reference an accepted repository”). gittr needs a relay that:

1. Accepts the small set of **Nostr-git / gittr** kinds below  
2. Stays **readable** and (optionally) **writable** for those kinds without forcing every user into the invite tree  
3. Still uses pyramid membership for chat / social spam control  

## NIPs & kinds used in gittr-style workflows

| Kind | Spec / origin | Purpose |
|------|---------------|---------|
| `0` | NIP-01 | Profiles |
| `3` | NIP-02 | Contact lists / WoT |
| `5` | NIP-09 | Deletions |
| `7` | NIP-25 | Reactions (stars) |
| `50` | gitnostr | Repository permissions |
| `51` | gitnostr | Legacy repository announce |
| `52` | gitnostr / gittr | **SSH public keys** (bridge `authorized_keys`) |
| `1111` | NIP-22 | Comments on issues/PRs/discussions |
| `1337` | NIP-C0 | Code snippets |
| `1617`–`1619` | NIP-34 | Patches, PRs, PR updates |
| `1621` / `1624` | NIP-34 / experimental | Issues, cover notes |
| `1630`–`1633` | NIP-34 | Status (open / applied / closed / draft) |
| `1985` | NIP-32 | Label overlays |
| `9735` | NIP-57 | Zap receipts |
| `9806` | gittr | Issue bounties |
| `10011` | NIP-39 | External identities (`i` tags) |
| `10018` | NIP-51 | Followed git repositories list |
| `10317` | NIP-34 | Preferred GRASP / git servers |
| `30023` | NIP-23 | Long-form discussion topics |
| `30617` / `30618` | NIP-34 | Repository announcement + state |
| `15128` / `35128` | NIP-5A | Nostr pages / nsite |

## Recommended settings (UI → general)

**Allowed kinds:** leave empty (defaults include the forge kinds above), or `all`, or deltas such as `+52,+10011,+10018,+9806,+1337,+1624,+50,+51`.

**Open kinds (anyone may publish)** — paste:

```
0,3,5,7,50,51,52,1111,1337,1617,1618,1619,1621,1624,1630,1631,1632,1633,1985,9735,9806,10011,10018,10317,30023,30617,30618,15128,35128
```

Empty open kinds = upstream behaviour (members-only writes).

Open kinds must also be **allowed**. Membership still gates ordinary notes unless you put those kinds in the open list.

## GRASP

Optional. When enabled, patch/issue events must reference a **30617** already stored on **this** relay.  

- **Event-propagation relay for gittr:** leave GRASP **off**; keep git hosting on the gittr bridge (`git.gittr.space`).  
- **Combined GRASP+relay host:** enable GRASP and open-write `30617` first.

## Point gittr at this relay

Add `wss://relay.yourdomain` to default / user relays and to Settings → SSH Keys fallback relays so kind-52 lists do not depend only on general relays (damus/nos.lol).

## Install note

Upstream `easy.sh` installs **stock** Pyramid from fiatjaf. For this adaptation, build/deploy from **this** repository / branch (see upstream README for build steps: `just build`, etc.).
