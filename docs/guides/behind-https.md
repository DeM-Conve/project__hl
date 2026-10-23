# Put hl behind HTTPS

hl v0.1 serves plain HTTP. That's fine on `localhost`. To reach your apps over HTTPS - from the
internet, or because an app requires a secure origin - put your own TLS-terminating proxy in
front of hl's proxy. hl stays as it is; the front proxy holds the certificate.

```
browser --https--> your TLS proxy (Caddy, nginx, a tunnel, ...) --http--> hl's proxy --> apps
```

## 1. Keep hl on loopback, on a spare port

```yaml
proxy:
  bind: 127.0.0.1        # only the front proxy talks to hl
  port: 8080             # the front proxy takes 80 and 443
  domain: apps.example.com
```

`domain` is the public name; with host routing apps are at `<app>.apps.example.com`, with path
routing at `apps.example.com/<app>/`. Restart: `hl proxy down && hl up`.

## 2. Front proxy

**Caddy** (gets and renews a certificate on its own):

```
apps.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

For host routing, use a wildcard site (`*.apps.example.com`), which needs Caddy's DNS challenge,
or list each app's name.

**nginx:**

```nginx
server {
    listen 443 ssl;
    server_name apps.example.com;
    ssl_certificate     /etc/ssl/apps.example.com/fullchain.pem;
    ssl_certificate_key /etc/ssl/apps.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;     # websockets
        proxy_set_header Connection "upgrade";
    }
}
```

Keep the `Host` header: host routing matches on it.

**Tunnels** (Cloudflare Tunnel, Tailscale Serve/Funnel and similar) work the same way: point
them at `http://127.0.0.1:8080` and keep the host name.

## 3. Tell the apps their public URL

Two things are still `http` from hl's point of view:

- `${HL_URL}` is built as `http://<domain>:<port>/...`, so it says `http://apps.example.com:8080`.
  For apps that need their public URL, set it explicitly instead of using `${HL_URL}`:

  ```yaml
  apps:
    my-app:
      env:
        MY_APP_URL: https://apps.example.com${HL_PREFIX}
  ```

- hl's Traefik doesn't trust forwarded headers from the front proxy, so apps see
  `X-Forwarded-Proto: http`. Apps that decide "am I on HTTPS?" from that header need their URL
  setting (above) or their own "trust proxy" option.

## Security checklist

- Every app reachable through the front proxy is public. hl adds no login; rely on the apps'
  own, or add authentication in the front proxy (basic auth, forward auth, an access policy of
  your tunnel).
- Keep `proxy.bind: 127.0.0.1` so nothing reaches hl around the front proxy.
- Leave the Traefik dashboard off.

Built-in HTTPS (local CA or ACME) is on the [product plan](../product-plan.md) for after v0.1.
