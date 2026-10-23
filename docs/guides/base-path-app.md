# Add an app that needs a base path

With path routing (the default) an app is served at `http://localhost/<app>/`. The proxy strips
`/<app>` before the request reaches the app, so the app serves from `/` as usual. What breaks is
the links the app **builds**: an absolute `/style.css` or a redirect to `/login` leaves the
prefix out, and the browser asks the proxy for `http://localhost/style.css`, which isn't the app.

Symptoms: the first page loads but without styles or scripts; clicking a link or logging in
lands on a 404; redirects go to `http://localhost/...` without the app's name.

The fix is to tell the app its prefix. hl gives you the value as `${HL_PREFIX}` (`/<app>`); you
pass it to the app's own setting. This guide uses [examples/base-path-app](../../examples/base-path-app/),
which reads `BASE_PATH`.

## 1. Find the app's setting

Most web apps have one. Common names: `BASE_PATH`, `BASE_URL`, `ROOT_PATH`, `ROOT_URL`,
`PUBLIC_URL`, `APP_URL`, `SUBPATH`, `--base-href` (Angular), `basePath` (Next.js, at build
time), `SCRIPT_NAME` (WSGI apps). Some want only the path (`/my-app`), some the full URL
(`http://localhost/my-app`): hl provides both, `${HL_PREFIX}` and `${HL_URL}`.

If the app's compose file doesn't pass that setting through yet, that is the one change the app
itself needs - and it's a normal, optional variable that does nothing without hl:

```yaml
# the app's compose.yml
services:
  web:
    environment:
      BASE_PATH: ${BASE_PATH_APP_BASE_PATH:-}
```

The `:-` default keeps `docker compose up` on its own working exactly as before.

## 2. Feed it from hl.yml

```sh
hl add ~/code/base-path-app          # or the examples/base-path-app folder of this repository
```

Then add `env` to the app in `hl.yml`, next to what `hl add` wrote:

```yaml
apps:
  base-path-app:
    path: ~/code/base-path-app
    service: web
    port: 8080
    env:
      BASE_PATH_APP_BASE_PATH: ${HL_PREFIX}     # -> /base-path-app
```

`env` values are compose interpolation variables: they fill in `${...}` in the app's compose
file. They don't reach the container unless the compose file passes them on, as in step 1.

## 3. Start and check

```sh
hl up base-path-app
hl config base-path-app | grep BASE_PATH   # BASE_PATH: /base-path-app
curl -s http://localhost/base-path-app/ | grep -o 'href="[^"]*"'
```

Every link should start with `/base-path-app/`.

## When there's no setting

Some apps can't run under a prefix at all (single-page apps built with absolute asset paths and
no option, for example). Use host routing for those:

```sh
hl add ~/code/my-app --routing host     # http://my-app.localhost
```

or set `routing: host` on an app you already added. Host routing needs nothing from the app, but
`<app>.localhost` isn't `localhost`: OAuth providers that allow plain-`http` redirect URIs only
for `localhost` won't accept it.
