# pyramid

recreate the social ladder

## how it works

Relay members can invite others to join. The system supports three internal actions: **invite** adds a user to the whitelist with the inviter as their parent, **remove** removes a user's invitation if they were invited by the remover, and **drop** recursively removes a user and all their descendants from the whitelist (a user can have more than one parent, which ensures they will keep existing even if one of their ancestors deletes them or is dropped).

## running

- You must install [templ](https://templ.guide) for generating the HTML templates.
- [just](https://just.systems), [entr](https://eradman.com/entrproject/) and [fd](https://github.com/sharkdp/fd) are also helpful.
- [godotenv](https://github.com/joho/godotenv) is used to read the `.env` file when developing.

```
PORT=3002
DOMAIN=0.0.0.0:3002
RELAY_NAME="myfriends"
RELAY_DESCRIPTION="a relay for me and my friends"
RELAY_PUBKEY="<my-pubkey-as-hex>"
RELAY_ICON="https://www.vectorkhazana.com/assets/images/products/Friend1.jpg"
MAX_INVITES_PER_PERSON=10
```

### development

To run the project call `just dev` (watches for changes and restarts automatically) or `templ generate && godotenv go run .`.

### production

For production builds, use `just build` to create a static binary (if you have `musl`, otherwise just run `templ generate && go build`).

Copy the binary to your server (say, to a directory at `/root/pyramid`) by running `scp ./pyramid server:/root/pyramid/pyramid` and run it with systemd by adding a file like this to `/etc/systemd/system/pyramid.service`:

```
[Service]
ExecStart=/root/pyramid/pyramid
Restart=no
StandardOutput=journal
StandardError=journal
SyslogIdentifier=pyramid
User=myname
Group=myname
WorkingDirectory=/root/pyramid
Environment=PORT=4002 DOMAIN=pyramid.myname.com RELAY_NAME=whatever ETC='fill the other values'

[Unit]
StartLimitIntervalSec=10min
StartLimitBurst=10

[Install]
WantedBy=multi-user.target
```

Then run `systemctl enable pyramid` and `systemctl start pyramid`.
