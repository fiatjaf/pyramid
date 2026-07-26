# Forge adaptations (gittr / Nostr git)

**This repository is an adapted [fiatjaf/pyramid](https://github.com/fiatjaf/pyramid).**  
It is maintained for **Nostr git** workflows used by **[gittr](https://gittr.space)** and related clients — not as “stock Pyramid”.

| | |
|--|--|
| Upstream | Community **membership** relay (invite ladder). Great product; different default goal. |
| This fork / gittr deploy | **Open** relay + **GRASP** companion for Nostr-git clients. Not invite/paid write. |
| Live | **`wss://relay.gittr.space`** (also in gittr `NEXT_PUBLIC_NOSTR_RELAYS` / bridge relays) |
| Repo name | Kept as `pyramid` for upstream tracking; branding is README / GitHub About. |

## “Members” — what that word means (and why we ignore it)

Upstream Pyramid only lets **invited members** publish to the main relay. That is the ladder/community model.

For gittr we **bypass** that with **`open_kinds_spec`**: every kind in that list can be published by **anyone**.  
Membership is **optional** — not the access model. **Private todos/discussions** stay **per-repo ACL on gittr**, not relay whitelist.

## Visibility vs junk

- **Want:** notes, discussions, issues/PRs, SSH keys, pages, apps, **follow lists**, **Cashu/nutzaps**, GRASP git traffic.
- Prefer a curated allow-list over “members only” if storage ever hurts — don’t block Cashu/NWC kinds we plan to use.

## NIPs & kinds (gittr workflows)

| Kind | Spec / origin | Purpose |
|------|---------------|---------|
| `0` / `1` / `3` / `5` / `6` / `7` | NIP-01/02/09/18/25 | Profile, notes, **follow/contact lists (kind 3)**, delete, repost, reactions |
| `50` / `51` / `52` | gitnostr / gittr | Permissions, legacy repo, **SSH keys** |
| `1111` / `30023` | NIP-22 / NIP-23 | Comments + discussion topics |
| `1337` | NIP-C0 | Code snippets |
| `1617`–`1621`, `1624`, `1630`–`1633` | NIP-34 | Patches, PRs, issues, cover, status |
| `1985` | NIP-32 | Labels |
| `3063` / `30063` / `32267` | NIP-82 / Zapstore | **App** asset / release / application announces |
| `7374` / `7375` / `7376` / `17375` | NIP-60 | **Cashu** quote / tokens / spend history / wallet |
| `9321` / `10019` | NIP-61 | **Nutzaps** + nutzap receive info |
| `23194` / `23195` | NIP-47 | **NWC** request/response (wallet connect) |
| `9735` | NIP-57 | Lightning zap receipts |
| `9806` | gittr | Bounties |
| `10011` | NIP-39 | External identities |
| `10018` | NIP-51 | **Followed git repos** list (repo follow list) |
| `10317` | NIP-34 | Preferred GRASP servers |
| `15128` / `35128` | NIP-5A | **Nostr Pages / nsite** manifests |
| `24242` | Blossom | Upload auth (pages) |
| `30617` / `30618` | NIP-34 | Repo announcement + state |

### Follow lists — yes

- **Kind `3`** — NIP-02 contact / people follow list (in open kinds).  
- **Kind `10018`** — followed **git repositories** (in open kinds).  

## Recommended production settings (open relay)

**Allowed kinds:** leave empty (defaults) **or** `all` if you want maximum community rerouting.

**Open kinds (anyone may publish)** — paste so it is **not** member-only  
(spaces after commas so the line wraps in GitHub README viewers):

```
0, 1, 3, 5, 6, 7, 50, 51, 52, 1111, 1337, 1617, 1618, 1619, 1621, 1624, 1630, 1631, 1632, 1633, 1985, 3063, 7374, 7375, 7376, 9321, 9735, 9806, 10011, 10018, 10019, 10317, 15128, 17375, 23194, 23195, 24242, 30023, 30063, 30617, 30618, 32267, 35128
```

Open kinds must also be **allowed**. Parser accepts spaces.

**GRASP:** **on** for this adaptation (see below).

## GRASP (recommended on)

This relay **should** run with **GRASP enabled**. It is a Nostr-git companion: clients (ngit, gittr, etc.) can use it as a `10317` / clone endpoint **and** as a normal event relay.

| | |
|--|--|
| What GRASP adds | HTTP git hosting for repos that have a **kind 30617** on **this** relay (`/grasp/…`) |
| What it does **not** do | Replace `git.gittr.space` (git-nostr-bridge / SSH). Both can coexist. |
| Does it hurt “catching” events? | **No for reads.** `REQ` / sync / stored notes, follows, SSH keys, Cashu, zaps still work. |
| Write-side nuance | With GRASP **on**, patches/issues/PRs/state (`1617`–`1633`, `30618`) must reference a **30617 already on this relay** (normal GRASP). Publish the repo announce here first (open kinds include `30617`). Orphaned git events without a local announce are rejected — that is intentional integrity, not spam filtering. |
| GRASP **off** | This fork skips that strict check so the box can be events-only; git HTTP hosting is disabled. |

gittr’s primary bare-repo/SSH path remains **`git.gittr.space`**. Enabling GRASP here helps **interop** (other clients that speak GRASP) and discovery — it does not stop the relay from storing general Nostr traffic.

## Point gittr at this relay

Already live: **`wss://relay.gittr.space`**

Wire into:

- `NEXT_PUBLIC_NOSTR_RELAYS` / `RELAYS`
- bridge `git-nostr-bridge.json` → `relays`
- Settings → SSH Keys fallback list  

## Install / build

```bash
curl -s https://raw.githubusercontent.com/arbadacarbaYK/pyramid/master/easy.sh | bash
```

That installs **this** fork (clone + `go build` with CGO). Override with `PYRAMID_REPO_URL` / `PYRAMID_REPO_REF` if needed.

Behind nginx (gittr production style): `HOST=127.0.0.1` `PORT=3334`, proxy `https`/`wss` to that port, set domain `relay.gittr.space`, paste open kinds, enable GRASP in settings.
