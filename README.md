# hl

**Run your own Docker Compose apps behind one local proxy, from their own folders, without
touching their compose files.**

[![release](https://img.shields.io/github/v/release/DeM-Conve/project__hl?sort=semver)](https://github.com/DeM-Conve/project__hl/releases/latest)
[![go](https://img.shields.io/github/go-mod/go-version/DeM-Conve/project__hl)](go.mod)
[![platforms](https://img.shields.io/badge/platforms-linux%20%7C%20macOS-blue)](#install)
[![license](https://img.shields.io/github/license/DeM-Conve/project__hl)](LICENSE)

You have a few projects, each with its own `compose.yml`. Each one wants port 80 or 8080, each
keeps its data in a different Docker volume, and you start them one by one. hl puts them all
behind one [Traefik](https://traefik.io) proxy at `http://localhost/<app>/`, keeps every app's
data in one `data/` folder, and gives you one set of commands, while each project stays an
ordinary compose project that still runs on its own.

```console
$ hl init ~/homelab
wrote /home/you/homelab/hl.yml
default homelab: /home/you/homelab
next: hl add <app folder>

$ hl add ~/code/blog
added blog (service web, port 8080)
url: http://localhost/blog/
note: path routing serves it under /blog, with the prefix stripped. The app must build its
links for that prefix: map ${HL_PREFIX} to its base-path setting in hl.yml (apps.blog.env),
or use --routing host.
start it: hl up blog

$ hl add ~/code/tracker
added tracker (service frontend, port 80)
url: http://localhost/tracker/
...

$ hl up
==> proxy
 ✔ Container hl-proxy-traefik-1  Started
==> blog
 ✔ Container hl-blog-db-1   Started
 ✔ Container hl-blog-web-1  Started
==> tracker
 ✔ Container hl-tracker-api-1       Started
 ✔ Container hl-tracker-frontend-1  Started
proxy running  127.0.0.1:80  home /home/you/homelab

APP      STATE   HEALTH   URL                        FOLDER
blog     up 2/2  healthy  http://localhost/blog/     /home/you/code/blog
tracker  up 2/2  -        http://localhost/tracker/  /home/you/code/tracker

$ ls ~/homelab/data
blog  tracker
```

(Shortened: the `==>` sections show docker compose's own output, which varies by version.)

## Contents

- [Why hl](#why-hl) · [Quickstart](#quickstart) · [Install](#install) · [How it works](#how-it-works)
- [Routing](#routing) · [hl.yml reference](#hlyml-reference) · [Commands](#commands)
- [Data and backups](#data-and-backups) · [Security](#security) · [Guides](#guides)
- [Troubleshooting and FAQ](#troubleshooting-and-faq) · [Uninstall](#uninstall)
- [Contributing](#contributing) · [License](#license)

## Why hl

hl is for one machine running **your own** apps: projects you develop or check out, each already
with a compose file. It is a small CLI with no daemon, no database and no web UI. What it does:

- **Your folders, untouched.** An app is any folder with a compose file. hl never writes into it;
  it layers a generated override on top at run time. The app keeps working with plain
  `docker compose up`, for you and for anyone else who uses it.
- **One proxy, path routing on `localhost`.** Every app is at `http://localhost/<app>/` by
  default. One host name matters for OAuth: Google sign-in, for example, accepts plain-`http`
  redirect URIs only on exactly `localhost`, not `<app>.localhost`. Host routing is there too.
- **One `data/` folder.** Every named volume becomes a plain folder, `data/<app>/<volume>`.
  Backing up the homelab is copying one folder; removing an app never deletes its data.
- **One file.** `hl.yml` describes the proxy and the apps, by hand or with `hl add`, comments kept.

How it compares (each of these is good at what it is for; pick the one that fits):

| Tool | What it is | Pick it over hl when |
| --- | --- | --- |
| [Harbormaster](https://gitlab.com/stavros/harbormaster) | Runs compose apps from git repos listed in one YAML file | you deploy from repos on a server and bring your own proxy |
| [Dockge](https://github.com/louislam/dockge) | Web UI to write and manage compose stacks in one stacks folder | you want a web UI and are happy to keep stacks in its folder |
| [Komodo](https://github.com/moghtech/komodo) | Build and deploy platform for many servers, with a web UI | you run several servers or want builds and deploys from a UI |
| [Portainer](https://www.portainer.io) | Web UI for containers, stacks and environments | you want a general container dashboard |
| [Dokku](https://dokku.com) / [Coolify](https://coolify.io) | Self-hosted PaaS: push to deploy, domains, TLS | you want Heroku-style deploys with HTTPS on a public server |
| [CasaOS](https://casaos.zimaspace.com) | Home-server OS layer with an app store | you want to install packaged apps from a store |

hl's niche: your own project folders, compose files left exactly as they are, path routing on
`localhost` (OAuth-friendly), and one `data/` folder for everything. It is not a PaaS: no web UI,
no app store, no multi-server, and no HTTPS of its own in v0.1 (see
[behind HTTPS](docs/guides/behind-https.md)).

## Quickstart

Needs Linux or macOS (on Windows, run hl inside WSL 2) and Docker with the compose plugin
v2.24 or newer.

```sh
# 1. install (to ~/.local/bin; see Install for other ways)
curl -fsSL https://raw.githubusercontent.com/DeM-Conve/project__hl/main/install.sh | sh
hl doctor                       # checks Docker and compose

# 2. set up a homelab folder (config + data): hl init asks where things go, finds the
#    apps in your projects folder (e.g. ~/code) and lets you pick which to add
hl init ~/homelab
#    or with no questions:  hl init ~/homelab --projects ~/code --yes
#    or one app at a time:  hl add ~/code/my-app

# 3. start everything (the proxy first); --wait also waits for healthchecks
hl up --wait

# 4. open it
hl open my-app                  # http://localhost/my-app/
```

If the app's page loads but its links, styles or redirects are broken, it needs its base path:
see [path routing](#routing) or use `hl add --routing host`. To try hl without an app of your
own, use the two apps in [examples/](examples/).

Later, when a project folder appears or is deleted, `hl sync` brings `hl.yml` up to date: it
shows what it would add, remove or re-detect, and asks before changing anything.

## Install

hl runs on **Linux and macOS** (amd64 and arm64) and needs Docker with the **compose plugin
v2.24+**. Windows isn't supported: there hl only prints a short error saying so (`hl version`
and `hl completion` still work). Run hl inside WSL 2 instead, with Docker available there.

**Install script** (downloads the release for your OS and CPU, verifies it against the release's
`checksums.txt`, installs to `~/.local/bin`, or `/usr/local/bin` as root):

```sh
curl -fsSL https://raw.githubusercontent.com/DeM-Conve/project__hl/main/install.sh | sh
```

Pick a version or folder with `--version` and `--dir` (or `HL_VERSION`, `HL_INSTALL_DIR`); sudo
is used only if the folder isn't writable:

```sh
curl -fsSL https://raw.githubusercontent.com/DeM-Conve/project__hl/main/install.sh | sh -s -- --version v0.1.0 --dir /usr/local/bin
```

**With Go** (1.21+):

```sh
go install github.com/DeM-Conve/project__hl/cmd/hl@latest
```

**By hand:** download the archive for your platform from
[Releases](https://github.com/DeM-Conve/project__hl/releases), check it, and put `hl` on your PATH:

```sh
sha256sum -c --ignore-missing checksums.txt     # macOS: shasum -a 256 -c --ignore-missing checksums.txt
tar -xzf hl_*_linux_amd64.tar.gz hl && install -m 0755 hl ~/.local/bin/hl
```

Each release also carries an SPDX SBOM per archive (`*.sbom.json`).

**Homebrew:** planned, not available yet.

Then run `hl doctor`, and enable shell completion if you like:
`source <(hl completion bash)` (also `zsh`, `fish`; add it to your shell's rc file). Completion
offers your app names and, where a command takes one, the app's services.

## How it works

- **The homelab folder** holds `hl.yml` (the proxy and the apps), `data/` (every app's data) and
  `.hl/` (files hl generates; safe to delete). `hl init` also writes a `.gitignore` for `data/`
  and `.hl/`, so you can keep `hl.yml` in git.
- **Finding the homelab:** `--home`, then `$HL_HOME`, then the nearest folder at or above the
  current one that has an `hl.yml`, then the default `hl init` recorded.
- **An app** is any folder with a compose file. `hl add` finds the service that publishes a port
  and pins it, with its container port, in `hl.yml`. The proxy forwards to that service, which no
  longer publishes a host port. Each app runs as its own compose project, `hl-<app>` (or
  `hl-<name>-<app>` in a homelab with a [`name`](#name-several-homelabs-on-one-docker)).
- **Nothing in the app is edited.** On every command hl writes `.hl/<app>/override.yml` and passes
  it to `docker compose` after the app's own files. `hl config <app>` shows the merged result.
- **hl doesn't parse compose files.** It asks `docker compose config` for the resolved model, so
  includes, extends, profiles and interpolation work as compose defines them.
- **No daemon.** hl reads `hl.yml` on every command and runs `docker compose`; the containers keep
  running (and restart with Docker) after hl exits.
- **`hl.yml` is checked strictly on every command.** Unknown keys (with a "did you mean"), wrong
  types, bad values and clashes between apps (same Docker project, URL prefix or host name) are
  all reported at once, each with its line, and the command stops. Earlier versions ignored some
  of these, so run `hl config check` before upgrading hl, and again right after.
- **Files are written whole.** `hl.yml`, the default-homelab note and every generated file are
  written to a temporary file first and then renamed into place, so a crash or Ctrl-C never leaves
  half a file. An existing file keeps its permissions, and a symlinked `hl.yml` stays a symlink.

## Routing

| `routing` | URL | Works with |
| --- | --- | --- |
| `path` (default) | `http://localhost/<app>/` | apps that can run under a base path |
| `host` | `http://<app>.localhost` | any app |
| `none` | not behind the proxy | databases, workers, apps that keep their own ports |

Set the default for the homelab with `proxy.routing`, and override it per app with `routing:`
(or `hl add --routing host`). A `proxy.port` other than 80 is added to the URLs
(`http://localhost:8080/<app>/`).

**Path routing.** The proxy matches `/<app>` and everything under `/<app>/`, redirects `/<app>`
to `/<app>/`, strips the prefix, and sends it along as `X-Forwarded-Prefix`. So the app serves
from `/` as usual, but the links it builds (assets, redirects, absolute URLs) must include the
prefix. Most apps have a setting for that; feed it from `hl.yml`:

```yaml
apps:
  my-app:
    path: ~/code/my-app
    env:
      MY_APP_BASE_PATH: ${HL_PREFIX}     # /my-app
      MY_APP_URL: ${HL_URL}              # http://localhost/my-app
```

`MY_APP_BASE_PATH` is a variable in the app's compose file (e.g.
`environment: BASE_PATH: ${MY_APP_BASE_PATH:-}`): hl's `env` values are compose interpolation
variables, and reach a container only through the app's compose file. Walkthrough:
[an app that needs a base path](docs/guides/base-path-app.md).

Path routes have no host-name match: an app answers on any name or address that reaches the proxy
(`localhost`, the machine's LAN IP, a name you give it).

**Host routing** needs `<app>.localhost` to resolve to your machine. Browsers do this on their
own; for other tools, or for a `proxy.domain` of your own, add the names to `/etc/hosts` or your
DNS.

## hl.yml reference

`hl init` writes a commented `hl.yml`. Everything except `apps.<name>.path` is optional; each
setting's default is in the tables below, and `hl config defaults` prints them with the values in
effect. hl reads and checks the file on every command; a problem stops the command (see
[`hl config check`](#version-and-editor-support)).

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/DeM-Conve/project__hl/main/schema/hl.schema.json
version: 1
# name: lab2               # only for a second homelab on the same Docker

proxy:
  bind: 127.0.0.1          # 0.0.0.0 to reach the apps from your network
  port: 80
  routing: path            # default for apps: path or host
  domain: localhost        # path: http://<domain>/<app>/  host: http://<app>.<domain>
  # network: hl-proxy      # Docker network the proxy shares with the apps (hl-<name>-proxy with name)
  # subnet: 172.31.0.0/16  # fixed subnet, so apps can trust the proxy by address
  # image: traefik:v3.6.25  # default: the Traefik version hl is tested with
  # project: hl-proxy      # the proxy's compose project (hl-<name>-proxy with name)
  # access_log: false      # Traefik's access log, shown by hl proxy logs
  # log_level: ERROR       # Traefik's log level
  # config: traefik.yml    # your own Traefik static config instead of the generated one
  # dynamic: proxy/dynamic.yml  # Traefik dynamic config (file provider) for routes that aren't hl apps
  # dashboard: false       # Traefik dashboard at http://traefik.<domain>, no password
  # dashboard_public: false  # required with dashboard when bind isn't loopback

env:                       # passed to every app; an app's own env wins
  TZ: UTC

apps:
  my-app:
    path: ~/code/my-app    # absolute, ~, or relative to the homelab folder
    service: web           # what the proxy forwards to (hl add picks it)
    port: 8080             # that service's port in its container (hl add picks it)
    # routing: host        # path, host or none
    # prefix: /tools/my-app          # path routing
    # host: my-app.example.test      # host routing
    # compose: [compose.yml, compose.prod.yml]
    # env_files: [my-app.env]
    # env: {KEY: value}
```

### `name`: several homelabs on one Docker

| Key | Default | What it does |
| --- | --- | --- |
| `name` | none | Sets this homelab apart from others on the same Docker daemon (a second homelab of yours, or another user's on the same machine). Lowercase letters, digits and `-`, starting with a letter or digit. |

Compose project and network names are global to a Docker daemon. Without `name`, apps run as
`hl-<app>` and the proxy as `hl-proxy` on the network `hl-proxy`, as they always have. With
`name: lab2`, apps run as `hl-lab2-<app>`, and the proxy's project and network default to
`hl-lab2-proxy`. Give the second homelab its own `proxy.port` too (two proxies can't both listen
on port 80), for example:

```yaml
name: lab2
proxy:
  port: 8080
```

hl never starts, stops or replaces a compose project whose containers were started from another
homelab's folder (it reads compose's `config_files` label): it refuses and says how to fix it
(give this homelab a `name`, or stop the other one), `hl status` shows such an app as
`other homelab`, and `hl down` leaves it alone with a warning. Adding `name` to a homelab that already runs
changes its project names: run `hl down` before adding it, then `hl up`.

### `proxy`

| Key | Default | What it does |
| --- | --- | --- |
| `bind` | `127.0.0.1` | Address the proxy listens on. `127.0.0.1` is this machine only; `0.0.0.0` (or a LAN IP) exposes every app to your network. |
| `port` | `80` | Host port of the proxy. Added to app URLs when not 80. |
| `routing` | `path` | Default routing for apps that don't set one: `path` or `host`. |
| `domain` | `localhost` | Host name in app URLs: `<domain>/<app>/` for path routing, `<app>.<domain>` for host routing. |
| `network` | `hl-proxy` (`hl-<name>-proxy` with `name`) | Docker network the proxy and each app's routed service share. |
| `subnet` | none (Docker picks) | Fixed subnet for that network, e.g. `172.31.0.0/16`, so an app can trust `X-Forwarded-For` from the proxy's address range. Before creating the network hl checks it doesn't overlap another Docker network or one of this machine's, and suggests a free one if it does. |
| `image` | `traefik:v3.6.25` | Traefik image. The default is the version hl is tested with; `hl doctor` warns when yours is older. v3.6.1+ is needed with Docker Engine 29. |
| `project` | `hl-proxy` (`hl-<name>-proxy` with `name`) | The proxy's compose project. Two homelabs on one machine need different projects, networks and ports: set [`name`](#name-several-homelabs-on-one-docker) and `port`, or all three of these. hl refuses to touch a proxy or network another homelab runs and says so. |
| `access_log` | `false` | Traefik's access log, in `hl proxy logs`. Not allowed with `config` (set `accessLog` in your file). |
| `log_level` | `ERROR` | Traefik's log level: `TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL` or `PANIC`. Not allowed with `config`. |
| `config` | none (generated) | Your own Traefik static config file, relative to the homelab folder, used instead of the generated `.hl/proxy/traefik.yml`. hl checks it has the `web` entrypoint on `:80` and `providers.docker` with `network:` set to `proxy.network`, and says what is missing. |
| `dynamic` | none | A Traefik dynamic config file (file provider) for routes that aren't hl apps: another machine, a NAS, redirects. It must be in a subfolder of the homelab (not the homelab folder itself, `data/` or `.hl/`), e.g. `proxy/dynamic.yml`: hl mounts that folder read-only into the proxy, and Traefik picks up edits without a restart. |
| `dashboard` | `false` | Serve the Traefik dashboard at `http://traefik.<domain>`. It has no password and shows every route, so it is off unless you turn it on. |
| `dashboard_public` | `false` | Required, in addition to `dashboard`, when `bind` isn't a loopback address (`127.0.0.1`). Without it, `dashboard: true` on such a bind is a problem in `hl.yml` and every command stops with it; with it, `hl doctor` warns that the dashboard is reachable from your network. |

**The proxy's Traefik config.** hl runs Traefik with one static config file,
`.hl/proxy/traefik.yml`, generated from these settings (entrypoints, the Docker provider, the
dashboard, logs, `ping` for the proxy's healthcheck); `hl config proxy` prints it. With `config`
set, your file is used instead. After changing proxy settings, `hl proxy up` applies them;
`hl proxy status` says when the running proxy is behind.

### `env`

A map of variables passed to every app, e.g. `TZ: UTC`. An app's own `env` wins over it.

### `projects`, `ignore`, `scan_depth`

Where `hl init` and `hl sync` look for apps. `hl init` writes `projects` from your answer.

```yaml
projects: [~/code]          # folders with your projects
ignore: [scratch-*, old]    # folder names sync never picks up
# scan_depth: 1             # 2 also looks in ~/code/<group>/<app>
```

| Key | Default | What it does |
| --- | --- | --- |
| `projects` | none | Folders holding your projects: absolute, `~/...`, or relative to the homelab folder. Each subfolder with a compose file (`compose.yaml`, `compose.yml`, `docker-compose.yaml`, `docker-compose.yml`) is an app, named after the folder (lowercased). A folder with a compose file is not looked into further. |
| `ignore` | none | Folder-name patterns (`*`, `?`, `[...]`) to skip. Always skipped as well: hidden folders (`.*`), `node_modules`, `vendor`, `__pycache__`, `venv`. |
| `scan_depth` | `1` | How many levels below a projects folder to look. |

A folder with a `.hlignore` file in it is skipped too, so a library or an experiment can opt out
without touching `hl.yml`. Ignoring only stops sync from adding a folder; an app already in
`hl.yml` stays. Apps outside the projects folders (added with `hl add`) are never changed by
sync, and an app whose projects folder is missing altogether (say, an unmounted drive) is not
removed.

### `version` and editor support

`version: 1` is the `hl.yml` format. A file without it (from before versions) keeps working;
the first time hl changes the file (`hl add`, `hl remove`, `hl set`, `hl unset`) it adds
`version: 1` at the top and says so. A file with a newer version than your hl understands is
refused with a message to update hl.

`hl init` starts `hl.yml` with a `# yaml-language-server: $schema=...` line pointing at
[schema/hl.schema.json](schema/hl.schema.json), so editors with the YAML language server (VS
Code's YAML extension, Neovim, Helix, ...) autocomplete keys, show what each one does, and flag
mistakes as you type. Add the line yourself to an older `hl.yml`.

`hl config check` lists every problem in `hl.yml` with its line, the key and a fix, including
unknown keys ("did you mean ..."). `hl set`, `hl unset` and `hl edit` change `hl.yml` only when
the result has no problems, and keep its comments.

### `apps.<name>`

App names use lowercase letters, digits, `.`, `_` and `-`, starting with a letter or digit. By
default `hl add` uses the folder's name. Reserved names (also `hl sync` and `hl init` skip
folders with them): `proxy`, whose compose project would be the proxy's, `hl-proxy` (likewise,
no app's `hl-<app>` may equal `proxy.project`), and the `hl config` subcommands, `check` and
`defaults`, so `hl config <app>` always reaches the app. `hl config defaults` lists them.

| Key | Default | What it does |
| --- | --- | --- |
| `path` | required | The app's folder: absolute, `~/...`, or relative to the homelab folder. |
| `service` | the service that publishes a port | Service the proxy forwards to. When several publish ports, `hl add` asks which one (the first of `frontend`, `web`, `app`, `nginx`, `proxy`, `ui` is the default); without a terminal it stops and asks for `--service`. `hl add` writes it here. |
| `port` | that service's published container port | Port inside the container. When the service publishes several, `hl add` asks (without a terminal: pass `--port`). `hl add` writes it here. |
| `routing` | `proxy.routing` | `path`, `host` or `none`. |
| `prefix` | `/<name>` | Path routing: the URL prefix, like `/tools/my-app` (no trailing slash). |
| `host` | `<name>.<domain>` | Host routing: the host name. |
| `compose` | what `docker compose` picks (`compose.yaml`, `compose.yml`, `docker-compose.yaml`, `docker-compose.yml`) | Compose files in the app's folder, in order. |
| `env_files` | none | Extra env files, relative to the homelab folder, read after the app's own `.env`. Handy for secrets you keep outside the app's folder. |
| `env` | none | Variables for the app's compose file. Values can use `${HL_URL}`, `${HL_PREFIX}`, `${HL_HOST}`, `${HL_DATA}`, `${HL_APP}` and your shell's variables. |

### Variables hl provides

| Variable | Value |
| --- | --- |
| `HL_APP` | the app's name |
| `HL_URL` | the app's URL without a trailing slash, e.g. `http://localhost/my-app` (empty for `routing: none`) |
| `HL_PREFIX` | the path prefix, e.g. `/my-app`; empty unless routing is `path` |
| `HL_HOST` | the host name: `<app>.<domain>` for host routing, else `domain` |
| `HL_DATA` | the app's data folder, `<homelab>/data/<app>` |

**Order:** a variable's value comes from, later winning: the app's `.env`, the app's
`env_files`, your shell, the shared `env`, the app's `env`.

### Built-in values

Every default above is defined once in hl's code and listed here. A few more values are fixed
today and not `hl.yml` settings (some have a command-line flag):

| Value | Used for |
| --- | --- |
| `hl-` | compose project name prefix: `hl-<app>`, `hl-proxy`; with `name`, `hl-<name>-<app>`, `hl-<name>-proxy` |
| `.hl/` | generated files in the homelab: `.hl/<app>/override.yml`, `.hl/proxy/compose.yml`, `.hl/proxy/traefik.yml` |
| `data/` | app data in the homelab: `data/<app>/<volume>` |
| `web` | the Traefik entrypoint name (container port 80) |
| `traefik` on `:8080` | Traefik's entrypoint for `/ping` (the proxy's healthcheck); not published |
| `/etc/traefik/traefik.yml`, `/etc/traefik/dynamic/` | where the static config and the folder of `proxy.dynamic` are mounted in the proxy |
| `traefik.<domain>` | the dashboard's host name, when `proxy.dashboard` is on |
| healthcheck every 10s, timeout 5s, 3 retries, start period 5s | the proxy container's healthcheck (`traefik healthcheck --ping`) |
| `unless-stopped` | the proxy container's restart policy |
| `200` | lines shown by `hl logs` (change with `--tail`) and `hl proxy logs` |
| `2m` | how long `--wait` waits for each app (change with `--wait-timeout`) |
| `20` | log lines `--wait` shows for each service that didn't become healthy |
| `8080` | the proxy port `hl init` offers when the default one is in use |
| `code`, `projects`, `src` | projects folders `hl init` suggests, under your home folder, when they exist |
| `.hlignore` | a folder holding a file with this name is never picked up as an app |
| `frontend`, `web`, `app`, `nginx`, `proxy`, `ui` | service names `hl add` prefers when several publish ports |
| `<config dir>/hl/home` | where `hl init` records the default homelab (`~/.config` on Linux, `~/Library/Application Support` on macOS) |

`hl config defaults` prints every setting that has a default, the value in effect and where it
comes from (`hl.yml`, default, flag or environment), including the homelab folder, the
editor `hl edit` runs (`$VISUAL`, then `$EDITOR`, else `vi`), and the built-in values above.

**Environment variables hl reads:** `HL_HOME` (the homelab folder) and `NO_COLOR` (no colours;
colours are also off when output isn't a terminal).

## Commands

Every command takes `--home <folder>` to pick the homelab. Commands that take `[app...]` act on
every app when none is named; they carry on past an app that fails and end with
`n of m apps failed: ...`. `hl up` and `hl down` are safe to repeat. `hl --help` and
`hl <command> --help` list every flag.

| Command | What it does |
| --- | --- |
| `hl init [folder]` | Set up a homelab folder (default: the current one) with `hl.yml`, `data/` and `.gitignore`, and make it the default. First checks Docker, compose and that the proxy port is free. On a terminal it asks for the projects folders, `path` or `host` routing, this machine only or your network, and which of the apps found to add; each has a flag: `--projects`, `--routing`, `--bind`, `--port`, `--apps a,b` (or `none`). With `--yes` or without a terminal it asks nothing and adds every app found. Never overwrites an existing `hl.yml`; on one it offers the new apps instead. |
| `hl sync` | Make `hl.yml` follow the `projects` folders. Shows a plan first: new folders with a compose file are added (service and port detected like `hl add`), apps whose folder is gone are removed (stopped if running; their data stays), apps whose pinned service or port left their compose file are re-detected, and folders it can't add are listed with the `hl add` command to use. Asks before applying; `--yes` applies without asking, `--dry-run` only shows. |
| `hl add <folder>` | Add an app. Flags: `--name`, `--routing path\|host\|none`, `--prefix`, `--host`, `--service`, `--port`, `--compose a.yml,b.yml`. When several services or ports could be the one to route, it asks on a terminal; without one it names the flag to pass. |
| `hl set <app> key=value...` | Change an app's settings in `hl.yml`: `routing`, `prefix`, `host`, `port`, `service`, `path`, `compose` and `env_files` (comma-separated), `env.NAME`. Keeps comments; writes only a valid result. `hl up <app>` applies it. |
| `hl unset <app> key...` | Remove an app's settings so their defaults apply again; `env.NAME` removes one variable, `env` all. |
| `hl edit` | Open `hl.yml` in `$VISUAL`/`$EDITOR` (default `vi`). You edit a copy in `.hl/edit/`; hl checks it when the editor closes and saves it only if it has no problems, else lists them and offers to reopen the editor. Works on a broken `hl.yml`. |
| `hl remove <app>` | Stop the app and remove it from `hl.yml`. Its data in `data/<app>` stays. |
| `hl status` (`ls`) | The proxy's state (and the dashboard's URL when it is on), then every app's STATE (`up 2/2`, `partial 1/2`, `down`, or `other homelab`), HEALTH (`healthy`, `starting: <services>`, `unhealthy: <services>`, or `-` when the app has no healthchecks), URL and folder. Says when `hl sync` has something to do. |
| `hl up [app...]` (`start`) | Start apps, the proxy first; builds images that don't exist yet. Before starting the proxy it checks the port is free (naming the program or container that holds it) and that `proxy.subnet` doesn't overlap another network. `--wait` waits until every container is running and healthy, and for an app that isn't, names its services and shows their last 20 log lines; `--wait-timeout` (default `2m`) is how long it waits for each app. Ends with the `hl status` table. |
| `hl down [app...]` (`stop`) | Stop and remove the apps' containers; with no app, also the proxy. Data stays. Apps another homelab runs are left alone. |
| `hl restart [app...]` | Restart containers without rebuilding, the proxy first (started if it isn't running), like `up`. |
| `hl rebuild [app...]` | Rebuild images from the apps' current code, then restart them. Takes `--wait` and `--wait-timeout` like `up`. |
| `hl update [app...]` | `git pull --ff-only` in each app's folder, then rebuild. Skips folders that aren't git repos or have uncommitted changes, and says which it skipped and why. Takes `--wait` and `--wait-timeout` like `up`. |
| `hl logs [app] [service...]` | The last 200 log lines of an app (or some of its services); with no app, every app's logs at once, each line prefixed by its app's name. `-n`/`--tail <lines>` changes how many; `-f`/`--follow` keeps following. |
| `hl ps [app]` | Containers of one app, or of the whole homelab. |
| `hl open <app>` | Print the app's URL and open it in the browser (only prints when there's no display, e.g. over ssh). |
| `hl shell <app> [service]` | A shell (`bash`, else `sh`) in a running container; default: the service behind the proxy (the pinned one, or the one hl detects). hl exits with the shell's own exit status. |
| `hl config <app>` | The app's full compose config as hl runs it: its own files plus hl's override. |
| `hl config proxy` | The proxy's Traefik static config (same as `hl proxy config`). |
| `hl config check` | Check `hl.yml`: every problem as `file:line:column: key: message - fix`, unknown keys with "did you mean". Exits 1 when there are problems. |
| `hl config defaults` | Every setting with a default, the value in effect, and where it comes from. |
| `hl path [app]` | The app's folder, or the homelab folder: `cd "$(hl path my-app)"`. |
| `hl proxy up\|down\|logs` | The proxy on its own; `logs` shows its last 200 lines. |
| `hl proxy status` | The proxy's state and health, image, listen address, config files, dashboard and access log; warns when the running proxy is behind `hl.yml` or its image is older than the tested Traefik. |
| `hl proxy config` | The Traefik static config the proxy runs with. |
| `hl doctor` | Check Docker, compose v2.24+, `hl.yml`, the proxy's Traefik config and that its project and network are this homelab's, and that every app's compose config resolves. Warns when `proxy.image` is older than the tested Traefik. |
| `hl version` (`--version`) | Version, commit, build date, Go version and platform, and whether the build had local changes (paste it into bug reports). `--short` prints only the version. |
| `hl completion bash\|zsh\|fish\|powershell` | Shell completion script. |

Planned, not built yet: `hl backup` and `hl restore` (see the
[product plan](docs/product-plan.md); until then, [back up by hand](docs/guides/backup.md)).

### Errors and exit codes

An error is one `error:` line saying what failed, usually followed by a `hint:` line saying what
to do. When a docker command fails, hl reads its output to explain the common causes: the
daemon isn't running, your user may not use Docker, the port is taken, compose is too old, the
subnet overlaps another network, an image can't be pulled, the disk is full. Warnings
(`warning: ...`) don't stop the command. All of these go to stderr.

| Exit code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | A command failed: one of hl's own checks (including problems in `hl.yml`), or a docker or git command. |
| `2` | Usage: an unknown command, a wrong argument or flag. |
| `128 + signal` | Interrupted; `130` for Ctrl-C. |
| the shell's | `hl shell` exits with the status of the shell it ran. |

**Ctrl-C** while docker runs: hl lets the docker command stop by itself (a Ctrl-C on the terminal
reaches it too; other signals are passed on), starts nothing more, and exits. A second Ctrl-C
stops it at once.

## Data and backups

Every named volume an app declares is stored as a plain folder, `data/<app>/<volume>`, created
by hl. Volumes the app marks `external`, or gives a non-local `driver` or `driver_opts`, are left
as the app defines them. Bind mounts in the app's compose file (`./uploads:/uploads`) stay where
the app puts them, inside its own folder.

- **Back up:** stop the apps (`hl down`), copy the homelab folder, start them again (`hl up`).
  Copy with ownership kept (`sudo rsync -a` or `sudo tar -cpzf`), since containers often own
  their files. For databases, a dump made with the database's own tool is the safer backup; many
  apps ship a backup service for that.
- **Restore:** put the folder back in place (same path), then `hl up`.
- **Remove an app:** `hl remove <app>` keeps `data/<app>`; delete it yourself when you're sure.

`hl backup` and `hl restore` commands are planned, not built yet.

Guide: [back up the homelab](docs/guides/backup.md).

## Security

- The proxy listens on `127.0.0.1` unless you set `proxy.bind`. Binding to `0.0.0.0` exposes
  every app to your network, with no authentication added by hl; see
  [reach your apps from your phone](docs/guides/lan-access.md).
- The Traefik dashboard is off by default, and refused on a non-loopback bind unless you also set
  `proxy.dashboard_public: true`.
- The proxy mounts the Docker socket read-only to discover apps; like any Docker tool, hl runs
  with your user's Docker rights.
- HTTP only: hl has no TLS in v0.1. Put it [behind your own HTTPS proxy](docs/guides/behind-https.md)
  if you reach it over an untrusted network.

Report vulnerabilities privately: [SECURITY.md](SECURITY.md).

## Guides

- [Add an app that needs a base path](docs/guides/base-path-app.md)
- [Move an existing compose app into hl](docs/guides/migrate-compose-app.md)
- [Reach your apps from your phone](docs/guides/lan-access.md)
- [Back up the homelab](docs/guides/backup.md)
- [Put hl behind HTTPS](docs/guides/behind-https.md)

## Troubleshooting and FAQ

Start with `hl doctor`: it checks Docker, compose, `hl.yml` and every app.

**`no homelab found`.** Run `hl init <folder>`, `cd` into a homelab folder, or set `HL_HOME` /
`--home`.

**`port 80 on 127.0.0.1 is already in use by ...`** (or docker's `port is already allocated` /
`address already in use`). Something else listens on the proxy's port; hl names the program or
container when it can. Stop it, or set `proxy.port: 8080` (URLs become
`http://localhost:8080/<app>/`). If it is another homelab's proxy, see
[`name`](#name-several-homelabs-on-one-docker).

**`proxy.subnet ... overlaps ...`.** The fixed subnet is used by another Docker network or by one
of this machine's networks. Pick the free subnet the hint suggests, or remove `proxy.subnet`.

**`hl.yml` problems after an upgrade.** hl checks `hl.yml` strictly on every command. Run
`hl config check`: it lists every problem with its line and a fix (unknown keys get a "did you
mean").

**`Docker project hl-... belongs to another homelab`.** Another homelab folder on this Docker
already runs an app or proxy under the same name. Give one of the homelabs a
[`name`](#name-several-homelabs-on-one-docker), or stop the other one
(`hl --home <its folder> down`).

**compose errors about `!reset`.** Your compose plugin is older than v2.24. Update Docker or the
compose plugin.

**`hl add` can't tell which service to route to.** Several services publish ports and none has a
common name. Pass `--service <name>` (and `--port` if it publishes several).

**The page loads but is unstyled, or links and redirects go to `http://localhost/...` without the
app's prefix.** The app doesn't know its base path. Set it from `${HL_PREFIX}`
([guide](docs/guides/base-path-app.md)), or use host routing for that app.

**404 from the proxy.** Check the app is up (`hl status`) and the URL ends where it should
(`/my-app/`). `hl proxy logs` shows Traefik's view; `hl config <app>` shows the labels hl added.

**`http://my-app.localhost` doesn't resolve.** Browsers resolve `*.localhost`; some other tools
don't. Use the browser, `curl --resolve`, or add the name to `/etc/hosts`.

**`http://traefik.localhost` gives 404.** The dashboard is off by default; set
`proxy.dashboard: true` and run `hl proxy up`.

**Permission denied in `data/`.** Containers often change the owner of their data folders. Use
`sudo` to read or copy them, or set the app's user id if it supports one (many images take
`PUID`/`PGID`).

**Can I still run an app without hl?** Yes. `docker compose up` in its folder works as before;
just don't run both at once (they would compete for the same port, and hl's copy keeps its data
in `data/`, not in the app's own volumes).

**Can I edit `hl.yml` by hand?** Yes; hl reads and checks it on every command, and keeps your
comments when `hl add`, `hl remove`, `hl set` or `hl unset` change it. `hl edit` checks your
changes before saving them.

**Does hl manage HTTPS, auth, or several machines?** No. See [Why hl](#why-hl) for tools that do,
and [behind HTTPS](docs/guides/behind-https.md).

**Windows?** Not supported: hl prints an error saying so (only `hl version` and `hl completion`
run). Use WSL 2 (e.g. Ubuntu) with Docker available inside it, and run the Linux build of hl
there.

## Uninstall

1. `hl down` stops every app and the proxy.
2. Delete the binary: `rm "$(command -v hl)"`.
3. Delete hl's note of the default homelab: `~/.config/hl` (macOS:
   `~/Library/Application Support/hl`).
4. Optionally remove the images hl used: the proxy's (`docker image rm traefik:v3.6.25`, or your
   `proxy.image`), plus the apps' images.

Your homelab folder, with `hl.yml` and every app's data, stays until you delete it. App folders
were never changed.

## Contributing

Bug reports, fixes and small features are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for
how to build, test and send changes (Conventional Commits), and the
[Code of Conduct](CODE_OF_CONDUCT.md). Releases: [RELEASING.md](RELEASING.md).
What's planned: [docs/product-plan.md](docs/product-plan.md).

## License

[MIT](LICENSE)
