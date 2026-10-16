# hl

Run your self-hosted apps behind one proxy. Each app is a folder with its own
`docker compose` file. hl doesn't change those files: it puts the app behind
[Traefik](https://traefik.io), stores its data in your homelab folder, and gives
you one set of commands for everything.

```sh
hl init ~/homelab            # create the homelab folder
hl add ~/code/my-app         # add an app
hl up                        # start everything
hl status                    # state and URLs
```

```
proxy running  127.0.0.1:80  home /home/me/homelab

APP      STATE        URL                        FOLDER
blog     up 2/2       http://localhost/blog/     /home/me/code/blog
tracker  up 3/3       http://localhost/tracker/  /home/me/code/tracker
```

## Install

```sh
go install github.com/DeM-Conve/project__hl/cmd/hl@latest
```

hl needs Docker with the compose plugin, v2.24 or newer. Run `hl doctor` to check
your setup.

## How it works

- **The homelab folder** holds `hl.yml` (the proxy and the apps), `data/` (every app's data) and
  `.hl/` (files hl generates). `hl init` makes it the default; `--home` or `$HL_HOME` pick another.
- **An app** is any folder with a compose file. `hl add` finds the service that publishes a port,
  and saves it to `hl.yml`. The proxy forwards to that service, which no longer publishes a host
  port. Each app runs as its own compose project, `hl-<app>`.
- **Data:** every named volume of the app is a plain folder, `data/<app>/<volume>`, so backing up
  the homelab means copying one folder. Volumes the app marks external, or gives a driver of its
  own, are left as they are.
- **Nothing in the app is edited.** On every command hl layers a generated override on top of the
  app's own files (`hl config <app>` shows the result).

## Routing

| `routing` | URL | Works with |
|---|---|---|
| `path` (default) | `http://localhost/<app>/` | apps that support a base path |
| `host` | `http://<app>.localhost` | any app |
| `none` | not behind the proxy | databases, workers, apps with their own ports |

Set it for the whole homelab under `proxy.routing`, or for one app with `routing:` (or
`hl add --routing host`).

Under path routing, the proxy strips the prefix before the request reaches the app, and sends it
as `X-Forwarded-Prefix`. The app still has to build its links for the prefix. Most apps have a
setting for that; pass it the prefix through `env`:

```yaml
apps:
  my-app:
    path: ~/code/my-app
    env:
      MY_APP_BASE_PATH: ${HL_PREFIX}     # /my-app
      MY_APP_URL: ${HL_URL}              # http://localhost/my-app
```

Path routing keeps every app on one host name. Google sign-in, for example, accepts plain `http`
redirect URIs only on exactly `localhost`, so host routing (`<app>.localhost`) can't use it.

## hl.yml

```yaml
proxy:
  bind: 127.0.0.1        # 0.0.0.0 to reach it from your network
  port: 80
  routing: path          # default for apps: path or host
  domain: localhost      # path: <domain>/<app>/, host: <app>.<domain>
  # network: hl-proxy    # Docker network the proxy shares with the apps
  # subnet: 172.31.0.0/16  # fixed, so apps can trust the proxy by address
  # image: traefik:v3.6

apps:
  my-app:
    path: ~/code/my-app  # absolute, ~, or relative to the homelab folder
    service: frontend    # what the proxy forwards to (hl add picks it)
    port: 80             # that service's port in its container
    # routing: host      # path, host or none
    # prefix: /tools/my-app        # path routing, default /<app>
    # host: my-app.example.com     # host routing, default <app>.<domain>
    # compose: [compose.yml, compose.prod.yml]  # default: what docker compose picks
    # env_files: [my-app.env]      # relative to the homelab folder, read after the app's own .env
    # env: {KEY: value}            # can use ${HL_URL} ${HL_PREFIX} ${HL_HOST} ${HL_DATA} ${HL_APP}
```

Settings reach the app's compose file in this order, later ones winning: the app's own `.env`,
then `env_files`, then `env`.

## Commands

| Command | What it does |
|---|---|
| `init [folder]` | create a homelab folder and make it the default |
| `add <folder>` | add an app (`--name`, `--routing`, `--prefix`, `--host`, `--service`, `--port`, `--compose`) |
| `remove <app>` | stop an app and remove it from hl.yml; its data stays |
| `status`, `ls` | the proxy, every app's state and URL |
| `up [app...]` | start apps (all by default), proxy first |
| `down [app...]` | stop apps; with no app also the proxy. Data stays |
| `restart [app...]` | restart without rebuilding |
| `rebuild [app...]` | rebuild images from the apps' current code |
| `update [app...]` | `git pull --ff-only` each app's folder (skips uncommitted changes), then rebuild |
| `logs <app> [service] [-f]` | logs |
| `ps [app]` | containers |
| `open <app>` | print the URL and open it in the browser |
| `shell <app> [service]` | shell in a container (default: the service behind the proxy) |
| `config <app>` | the compose config as hl runs it |
| `path [app]` | the app's folder, or the homelab folder |
| `proxy up\|down\|logs` | the proxy on its own |
| `doctor` | check docker, the config and every app |
| `completion bash\|zsh\|fish` | shell completion, e.g. `source <(hl completion bash)` |

The Traefik dashboard is at `http://traefik.<domain>`.

## Development

```sh
go test ./...
go build -o hl ./cmd/hl
```
