# Forge adaptations (gittr / Nostr git)

**This repository is an adapted [fiatjaf/pyramid](https://github.com/fiatjaf/pyramid).**  
It is maintained for **Nostr git** workflows used by **[gittr](https://gittr.space)** and related clients — not as “stock Pyramid”.

| | |
|--|--|
| Upstream | Community **membership** relay (invite ladder). Great product; different default goal. |
| This fork / gittr deploy | Run it as an **open** relay for forge + discussion + notes + Cashu/nutzaps. **Not** invite/paid write. |
| Repo name | Kept as `pyramid` for upstream tracking; branding is README / GitHub About. |

## “Members” — what that word means (and why we ignore it)

Upstream Pyramid only lets **invited members** publish to the main relay. That is the ladder/community model.

For gittr we **bypass** that with **`open_kinds_spec`**: every kind in that list can be published by **anyone**.  
Set open kinds to the full list below. You can leave the invite tree unused; root admin UI still works for settings.

Membership is **optional** here — not the access model. **Private todos/discussions** stay **per-repo ACL on gittr**, not relay whitelist.

## Visibility vs junk

- **Want:** notes, discussions, issues/PRs, SSH keys, pages, apps, **follow lists**, **Cashu/nutzaps** (for future repo zaps / bounties).
- **Fine to allow broadly** for rerouting: profiles, follows, reactions, Lightning zaps, deletes.
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

Already in this fork’s `SupportedKindsDefault` (plus upstream social/defaults). **`7374` added** for NIP-60 quotes.

### Follow lists — yes

- **Kind `3`** — NIP-02 contact / people follow list (in open kinds).  
- **Kind `10018`** — followed **git repositories** (in open kinds).  

Those are the two follow-list shapes gittr cares about.

## Recommended production settings (open relay)

**Allowed kinds:** leave empty (defaults) **or** `all` if you want maximum community rerouting.

**Open kinds (anyone may publish)** — paste so it is **not** member-only  
(spaces after commas so the line wraps in GitHub README viewers):

```
0, 1, 3, 5, 6, 7, 50, 51, 52, 1111, 1337, 1617, 1618, 1619, 1621, 1624, 1630, 1631, 1632, 1633, 1985, 3063, 7374, 7375, 7376, 9321, 9735, 9806, 10011, 10018, 10019, 10317, 15128, 17375, 23194, 23195, 24242, 30023, 30063, 30617, 30618, 32267, 35128
```

Open kinds must also be **allowed**. Parser accepts spaces.

## GRASP

Optional. For a **public event relay** next to gittr’s existing git host (`git.gittr.space`), leave GRASP **off**. Enable only if this box should also be a GRASP git server.

## Point gittr at this relay

Live: **`wss://relay.gittr.space`**

- `NEXT_PUBLIC_NOSTR_RELAYS` / bridge `relays` / SSH Keys fallback list  

## Install / build

Upstream `easy.sh` installs **stock** Pyramid. For this adaptation, build from **this** repo (`just build` / `go build` with CGO for LMDB).
