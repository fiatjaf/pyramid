# pyramid

recreate the social ladder

### run

```sh
go install github.com/fiatjaf/pyramid@latest
DOMAIN="example.com" RELAY_NAME="my relay" RELAY_PUBKEY=owner_pubkey_hex pyramid
```

### configuration

look at [example.env](./example.env) for all configuration options.

you can also manually edit the `users.json` file. do this only when the server is down.

`users.json` is formatted as follows:
```json
{ "[user_pubkey_hex]": "[invited_by_pubkey_hex]" }
```
