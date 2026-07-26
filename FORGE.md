# Forge adaptations (gittr / Nostr git)

**This repository is an adapted [fiatjaf/pyramid](https://github.com/fiatjaf/pyramid).**  
It is maintained for **Nostr git** workflows used by **[gittr](https://gittr.space)** and related clients — not as “stock Pyramid”.

| | |
|--|--|
| Upstream | Community **membership** relay (invite ladder). Great product; different default goal. |
| This fork / gittr deploy | Run it as an **open** relay for forge + discussion + normal notes. **We do not want member-only write.** |
| Repo name | Kept as `pyramid` for upstream tracking; branding is README / GitHub About. |

## “Members” — what that word means (and why we ignore it)

Upstream Pyramid only lets **invited members** publish to the main relay. That is the ladder/community model.

For gittr we **bypass** that with **`open_kinds_spec`**: every kind in that list can be published by **anyone**.  
Set open kinds to the full forge+discussion list below (or `allowed_kinds_spec=all` + matching open list). You can leave the invite tree unused; root admin UI still works for settings.

Membership is **optional** here — not the access model.

## Visibility vs junk

- **Want:** kind `1` notes, discussions (`30023`/`1111`), issues/PRs, SSH keys, pages, apps — so the relay is useful and gets added as a normal relay.
- **Fine to allow broadly** for rerouting/visibility: profiles, follows, reactions, zaps, deletes.
- **Low value for us** (can omit from *allowed* list if storage/CPU hurts): random live/group/cashu-only kinds we never read in gittr. Prefer a curated allow-list over “members only”.

Todos in gittr today still ride **NIP-34 issues/PRs** (and future NIP drafts) — not a separate closed chat silo. Discussion is **30023 + 1111**; kind **1** is also useful for general Nostr visibility.

## NIPs & kinds (gittr workflows)

| Kind | Spec / origin | Purpose |
|------|---------------|---------|
| `0` / `1` / `3` / `5` / `6` / `7` | NIP-01/02/09/18/25 | Profile, notes, follows, delete, repost, reactions |
| `50` / `51` / `52` | gitnostr / gittr | Permissions, legacy repo, **SSH keys** |
| `1111` / `30023` | NIP-22 / NIP-23 | Comments + discussion topics |
| `1337` | NIP-C0 | Code snippets |
| `1617`–`1621`, `1624`, `1630`–`1633` | NIP-34 | Patches, PRs, issues, cover, status |
| `1985` | NIP-32 | Labels |
| `3063` / `30063` / `32267` | NIP-82 / Zapstore | **App** asset / release / application announces |
| `9735` | NIP-57 | Zaps |
| `9806` | gittr | Bounties |
| `10011` | NIP-39 | External identities |
| `10018` | NIP-51 | Followed git repos |
| `10317` | NIP-34 | Preferred GRASP servers |
| `15128` / `35128` | NIP-5A | **Nostr Pages / nsite** manifests |
| `24242` | Blossom | Upload auth (pages) |
| `30617` / `30618` | NIP-34 | Repo announcement + state |

Already in this fork’s `SupportedKindsDefault` (plus upstream social/defaults).

## Recommended production settings (open relay)

**Allowed kinds:** leave empty (defaults) **or** `all` if you want maximum community rerouting.

**Open kinds (anyone may publish)** — paste so it is **not** member-only:

```
0,1,3,5,6,7,50,51,52,1111,1337,1617,1618,1619,1621,1624,1630,1631,1632,1633,1985,3063,9735,9806,10011,10018,10317,15128,24242,30023,30063,30617,30618,32267,35128
```

Open kinds must also be **allowed**.

## GRASP

Optional. For a **public event relay** next to gittr’s existing git host (`git.gittr.space`), leave GRASP **off**. Enable only if this box should also be a GRASP git server.

## Point gittr at this relay (when live)

Add e.g. `wss://relay.gittr.space` to:

- `NEXT_PUBLIC_NOSTR_RELAYS` / `RELAYS` in `.env` examples  
- bridge `git-nostr-bridge.json` `relays`  
- Settings → SSH Keys fallback list  

Until the hostname exists, keep it **commented** in examples so deploys don’t hang on a dead URL.

## Install / build

Upstream `easy.sh` installs **stock** Pyramid. For this adaptation, build from **this** repo (`just build` / `go build` with CGO for LMDB). Binary is not published from gittr’s CI yet — build on the relay host.
