# Reach your apps from your phone

By default the proxy listens on `127.0.0.1`: only the machine running hl can open the apps. To
use them from a phone or another computer on the same network, make the proxy listen on the
network.

**Before you do:** everyone on that network can then reach every app, over plain HTTP, with only
the apps' own logins in front of them. Do this on a network you trust (your home LAN), not on
public Wi-Fi, and not with a port forwarded from your router.

## 1. Listen on the network

In `hl.yml`:

```yaml
proxy:
  bind: 0.0.0.0          # every interface; or this machine's LAN address only, e.g. 192.168.1.20
```

Restart the proxy so it picks up the new address:

```sh
hl proxy down
hl up
```

## 2. Find the machine's address

```sh
hostname -I | awk '{print $1}'      # Linux
ipconfig getifaddr en0              # macOS (Wi-Fi)
```

## 3. Open an app

Path routing (the default) matches on the path only, so any address that reaches the proxy works:

```
http://192.168.1.20/my-app/
```

Use your machine's address. If the app builds absolute URLs from its own URL setting
(`${HL_URL}`, which is `http://localhost/...`), links may still point at `localhost`, which on
the phone means the phone. Set `proxy.domain` to the machine's address or a name your network
resolves (for example a name in your router's DNS), so `HL_URL` uses it:

```yaml
proxy:
  bind: 0.0.0.0
  domain: 192.168.1.20
```

Note: changing `domain` away from `localhost` affects OAuth sign-in that depends on the
`localhost` exception for plain-`http` redirect URIs.

**Host routing** (`http://my-app.<domain>`) needs names the phone can resolve: a wildcard DNS
entry in your router or local DNS server pointing `*.<domain>` at the machine. `*.localhost`
doesn't work from other devices.

## 4. Check the firewall

If the page doesn't load from the phone but does on the machine, a firewall is blocking port 80
(or your `proxy.port`). For example with ufw: `sudo ufw allow from 192.168.1.0/24 to any port 80
proto tcp`. On macOS, allow incoming connections for Docker when asked.

## The Traefik dashboard

It stays off unless `proxy.dashboard: true`. With a non-loopback `bind`, hl refuses to start the
proxy with the dashboard on unless you also set `proxy.dashboard_public: true` - the dashboard
has no login and shows every route and service.

## Going back

Set `bind: 127.0.0.1` (or remove the line), then `hl proxy down && hl up`.
