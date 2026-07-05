# backup & restore

pyramid can hand you a single-file backup of your whole relay, and take it back
to restore or migrate. everything lives in one `.tar.gz`.

## what a backup contains

- **every event layer** — `main`, `internal`, `personal`, `favorites`, `inbox`,
  `secret`, `moderated`, `moderation-queue`, `popular`, `uppermost`, `scheduled`,
  `invites`, `pending-access`, `blossom`, `operator` — each as a `<name>.jsonl`
  file (one event per line). your nip-29 groups live in `main`, so they come
  along for free.
- **`settings.json`** — all your relay settings, **including the relay's internal
  signing key**. keep the backup private: whoever has it can act as your relay.
- **`management.jsonl`** — the membership / invite-tree log.
- **`grasp-repos/`** — your grasp (nip-34) git repositories, on disk.
- **`blossom-files/`** — uploaded blossom media (this also covers nsite, whose
  files are blossom blobs).

two things are deliberately left out: the `system` layer (a re-derivable cache of
profiles and relay lists pulled from the network — it rebuilds itself, and it's
usually far bigger than everything else combined) and the legacy `groups` layer
(its events already live in `main`).

## downloading a backup

from the **settings page**, in the *system* section, click **download backup**.

or over http, with the export token (a random string kept at
`<data>/.export-token`, readable only by the operator):

```
curl -H "X-Export-Token: $(cat /path/to/data/.export-token)" \
  https://your-relay.example.com/export > backup.tar.gz
```

the endpoint accepts either that token **or** a logged-in root session (which is
how the settings-page button works). add `?blossom=0` to skip the (potentially
large) blossom media.

## restoring a backup

restoring makes the relay **become the backup**: settings (including the internal
**signing key**, so nip-29 groups keep their identity), the membership / invite
tree, grasp repos, blossom media and all events are replaced by the archive's.
the only thing that is *not* taken from the backup is the **host domain** — that
always follows the deployment (re-asserted from `PYRAMID_DOMAIN` on every boot),
so a backup from another host can't break your login.

> because the signing key comes back, restoring one relay's backup onto a second,
> still-running relay makes **both** sign with the same key. that's a deliberate
> migration/clone — don't do it by accident.

restore is a two-step, offline operation: **stage** the archive, then **fully
restart** the relay to apply it.

### 1. stage the archive

from the settings page (*system* → **backup** → **restore from backup**), or over
http:

```
curl -H "X-Export-Token: $(cat /path/to/data/.export-token)" \
  -F file=@backup.tar.gz \
  https://your-relay.example.com/restore
```

either way the archive is validated and written to `<data>/.restore.tar.gz`.
**nothing changes yet.** if you have filesystem access you can skip the upload
and just drop it there yourself:

```
cp backup.tar.gz /path/to/data/.restore.tar.gz
```

### 2. fully restart the pyramid process

```
# whatever runs pyramid, restart it for real:
docker restart pyramid      # or: systemctl restart pyramid, kill + relaunch, ...
```

> the **restart button on the settings page does not count** — it does an
> in-process soft restart that does not re-read the store, so it will **not**
> apply a staged restore. you must restart the actual process/container.

### what happens on that restart

pyramid finds `<data>/.restore.tar.gz`, puts `settings.json`, `management.jsonl`,
`grasp-repos/` and `blossom-files/` in place before it reads them, then imports
each `<name>.jsonl` into its event layer. **the relay does not serve until the
import finishes** — it is honestly "down, restoring". the event store fsyncs
every write, so a large relay can take a while (tens of thousands of events =
many minutes); watch the log for `restore: importing` → `restore: complete`. on
success the staged archive is removed and the relay serves the restored data.

the import is idempotent (already-present events are skipped), so an interrupted
restart just resumes on the next boot. a corrupt or truncated archive is renamed
to `.restore.failed` and the relay boots normally — a bad upload can never brick
the relay.

## running behind a reverse proxy

pyramid streams a restore upload straight to disk, so it handles arbitrarily
large backups itself. but if you put a reverse proxy in front of it (nginx,
caddy, a load balancer, an ingress controller…), the proxy usually gets in the
way of big uploads in two ways, and you'll see a **413 Request Entity Too Large**
or a stalled upload. tune the proxy:

- **lift the request body size limit.** nginx defaults to a tiny `1m` and will
  reject the upload before it ever reaches pyramid. set it to `0` (unlimited) or
  a ceiling big enough for your backups.
- **turn off request buffering.** by default nginx spools the *entire* upload to
  a temp file on the proxy before forwarding a single byte — so a 20gb backup
  needs 20gb of scratch space on the proxy. streaming forwards it as it arrives.
- **give it time.** a large upload over a slow link can outlast the default
  timeouts; raise the read/send timeouts.

an nginx location fronting the relay:

```nginx
location / {
    proxy_pass http://127.0.0.1:3334;
    client_max_body_size 0;          # 0 = unlimited (or e.g. 20g)
    proxy_request_buffering off;      # stream to pyramid, don't spool to disk
    proxy_read_timeout 1h;
    proxy_send_timeout 1h;
    # keep the websocket upgrade working for the relay itself
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_set_header Host $host;
}
```

caddy needs no body-size config (it has no default limit); a plain
`reverse_proxy 127.0.0.1:3334` works, and it streams by default.

> some proxies can't be coaxed into unlimited uploads (managed load balancers,
> the nginx-ingress controller's per-server cap, etc.). in that case skip the
> upload entirely: copy the archive onto the relay's disk as
> `<data>/.restore.tar.gz` and restart — the restore is applied on the next
> boot, no HTTP upload involved.
