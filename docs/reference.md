# Settings reference

Every setting hl has, with its default. The [README](../README.md) is the user guide; this page is
the detail behind it.

hl keeps the settings in `hl.yml` in the homelab folder, but you don't need to open it: `hl set`
changes a setting, `hl unset` puts its default back, and `hl show` prints the values in effect and
where each comes from (`hl.yml`, default, detected, flag or environment).

- [How `hl set` works](#how-hl-set-works)
- [App settings](#app-settings) · [Proxy settings](#proxy-settings) · [Homelab settings](#homelab-settings)
- [Variables hl provides](#variables-hl-provides) · [Env hl fills in](#env-hl-fills-in)
- [Several homelabs on one Docker (`name`)](#several-homelabs-on-one-docker-name)
- [The hl.yml file](#the-hlyml-file) · [Built-in values](#built-in-values)
- [Errors and exit codes](#errors-and-exit-codes)

## How `hl set` works

The first argument says what to change: an app's name, `proxy`, or nothing for the homelab itself.

```sh
hl set my-app routing=host host=my-app.example.test   # an app
hl set proxy port=8080 bind=0.0.0.0                    # the proxy
hl set env.TZ=Europe/Berlin                            # the homelab: env every app gets
hl set projects+=~/src ignore+='scratch-*' scan_depth=2

hl unset my-app prefix env.TZ                          # back to the defaults
hl unset proxy port
hl unset env.TZ
```

- `key=value` sets a value. `env.NAME=value` sets one variable; `hl unset ... env.NAME` removes
  it, `hl unset ... env` removes them all.
- Lists are comma-separated and `=` replaces the whole list (`projects=~/code,~/src`); `+=` adds
  entries and `-=` removes them (`ignore+=old`, `compose-=compose.dev.yml`).
- Switches take `true` or `false` (`hl set proxy debug=true`).
- Single quotes keep your shell from expanding `${...}`: `hl set my-app 'env.APP_URL=${HL_URL}'`.
- hl writes `hl.yml` only when the result has no problems, keeps its comments and order, prints
  what it changed, and says which command applies it (`hl up <app>`, `hl up proxy`, `hl sync`,
  or `hl down` then `hl up`).
- `hl add <folder> key=value...` takes the same app settings, plus `name`.
- `hl set --help` and shell completion list the keys of each target.

## App settings

`hl set <app> key=value`, `hl unset <app> key`, or on `hl add <folder> key=value`.

App names use lowercase letters, digits, `.`, `_` and `-`, starting with a letter or digit. By
default `hl add` uses the folder's name, less a [`strip_prefix`](#where-hl-init-and-hl-sync-look-for-apps).
`proxy` is reserved (`hl up proxy` and the like mean the proxy), and so is `hl-proxy`: no app's
compose project `hl-<app>` may equal the proxy's project. `hl init` and `hl sync` skip folders with
those names.

| Key | Default | What it does |
| --- | --- | --- |
| `name` | the folder's name, less a `strip_prefix` | The app's name. On `hl add` it names the new app; `hl set <app> name=<new>` (on its own) renames one: it keeps its settings, and its `data/<app>/` and `.hl/<app>/` move along. Refused while the app has containers (`hl down <app>` first), when `data/<new>/` exists, and for a name `hl add` would refuse. The new name changes the default `prefix` and `host`, and the Docker project. |
| `path` | required | The app's folder: absolute, `~/...`, or relative to the homelab folder. |
| `service` | the service that publishes a port | Service the proxy forwards to. When several publish ports, `hl add` asks which one (the first of `frontend`, `web`, `app`, `nginx`, `proxy`, `ui` is the default); without a terminal it takes that one and says so, and asks for `service=<name>` only when none has one of those names. `hl add` writes it. |
| `port` | that service's published container port | Port inside the container. When the service publishes several, `hl add` asks (without a terminal: pass `port=<port>`). `hl add` writes it. |
| `routing` | `proxy.routing` | `path`, `host` or `none` (not behind the proxy). |
| `prefix` | `/<name>` | Path routing: the URL prefix, like `/tools/my-app` (no trailing slash). |
| `host` | `<name>.<domain>` | Host routing: the host name. Use it to give one app its own host name (say `photos.example.com`) instead of `<name>.<domain>`. |
| `compose` | what `docker compose` picks (`compose.yaml`, `compose.yml`, `docker-compose.yaml`, `docker-compose.yml`) | Compose files in the app's folder, in order (a list). `compose+=compose.dev.yml` on an app that names none starts from the file compose picks, so it adds an override instead of replacing it. |
| `env.NAME` | none | Variables for the app's compose file (`env.MY_APP_BASE_PATH='${HL_PREFIX}'`). Values can use the [variables hl provides](#variables-hl-provides) and your shell's variables. They are compose interpolation variables: they reach a container only through the app's compose file (`environment: BASE_PATH: ${MY_APP_BASE_PATH:-}`). |

Applied by `hl up <app>`. An app that bakes a setting into its image as a build arg
(`build: args: BASE_PATH: ${MY_APP_BASE_PATH}`) gets that image rebuilt: after each `hl up` hl
records the app's resolved build settings in `.hl/<app>/builds.json`, and the next `hl up`
rebuilds the services whose build settings changed since. The first `hl up` only builds what is
missing, as compose does; an image built before hl kept track needs one `hl rebuild <app>`.

## Proxy settings

`hl set proxy key=value`, `hl unset proxy key`. Applied by `hl up proxy`; a change of `domain`,
`routing` or `port` also needs `hl up` (it is in the apps' routes or URLs), one of `network`,
`subnet` or `project` needs `hl down` then `hl up`, and hl says so. `hl show proxy` says when the running proxy is
behind its settings.

| Key | Default | What it does |
| --- | --- | --- |
| `bind` | `127.0.0.1` | Address the proxy listens on. `127.0.0.1` is this machine only; `0.0.0.0` (or a LAN IP) exposes every app to your network. |
| `port` | `80` | Host port of the proxy. Added to app URLs when not 80. |
| `routing` | `path` | Default routing for apps that don't set one: `path` or `host`. |
| `domain` | `localhost` | Host name in app URLs: `<domain>/<app>/` for path routing, `<app>.<domain>` for host routing. |
| `subnet` | none (Docker picks) | Fixed subnet for the proxy network, e.g. `172.31.0.0/16`. See below. |
| `image` | `traefik:v3.6.25` | Traefik image. The default is the version hl is tested with; `hl doctor` warns when yours is older. v3.6.1+ is needed with Docker Engine 29. Use it as the escape hatch when a Docker update needs a newer Traefik than this hl pins yet. |
| `debug` | `false` | Traefik's INFO log and its access log (one line per request), in `hl logs proxy`. Off, Traefik logs errors only. Turn it on to see why a URL gives 404 or which app a request reached. |

hl runs Traefik with one static config file, `.hl/proxy/traefik.yml`, generated from these
settings (entrypoints, the Docker provider, logs, `ping` for the proxy's healthcheck); `hl show
proxy` names it. hl doesn't run the Traefik dashboard.

The proxy and each app's routed service share one Docker network, and the proxy runs as its own
compose project; both are `hl-proxy`, or `hl-<name>-proxy` with a
[`name`](#several-homelabs-on-one-docker-name). They are not settings: a second homelab gets its
own by `hl set name=<x>`. `hl show proxy` prints them.

**`subnet`: an app that trusts the proxy by address.** Behind the proxy, every request reaches an
app from the proxy container's address, and the visitor's address is in the `X-Forwarded-For`
header. Apps that rate-limit, log or ban by client IP read that header only from proxies they
trust, usually given as an address range (a "trusted proxies" setting). Docker picks the network's
range itself and may pick another one when the network is recreated, so pin it and give the app
the same range:

```sh
hl set proxy subnet=172.31.0.0/16
hl set my-app 'env.MY_APP_TRUSTED_PROXIES=172.31.0.0/16'   # the variable your app's compose file reads
hl down && hl up                                           # the network is recreated with it
```

Before creating the network hl checks that the subnet doesn't overlap another Docker network or
one of this machine's, and suggests a free one if it does.

## Homelab settings

`hl set key=value` and `hl unset key`, with no app or `proxy` in front.

### Shared env

`hl set env.NAME=value` passes a variable to every app; an app's own `env.NAME` wins over it.
Without a `TZ` here, apps get this machine's time zone (see
[Variables hl provides](#variables-hl-provides)). Applied by `hl up`.

### Where hl init and hl sync look for apps

`projects`, `ignore`, `scan_depth` and `strip_prefix` tell `hl init` and `hl sync` where your
apps are and how to name them. `hl init` writes `projects` from your answer (or `--projects`) and
`strip_prefix` from `--strip-prefix`. Applied by `hl sync`.

```sh
hl set projects=~/code,~/src        # folders with your projects (each must exist)
hl set projects+=~/work             # add one; projects-=~/work removes it
hl set ignore+='scratch-*,old'      # folder names sync never picks up
hl set strip_prefix=project__       # folder project__blog becomes app blog
hl set scan_depth=2                 # also look in ~/code/<group>/<app>
hl unset ignore                     # back to the default (none)
```

| Key | Default | What it does |
| --- | --- | --- |
| `projects` | none | Folders holding your projects: absolute, `~/...`, or relative to the homelab folder (hl writes them that way). Each subfolder with a compose file (`compose.yaml`, `compose.yml`, `docker-compose.yaml`, `docker-compose.yml`) is an app, named after the folder (lowercased). A folder with a compose file is not looked into further. |
| `ignore` | none | Folder-name patterns (`*`, `?`, `[...]`) to skip. Always skipped as well: hidden folders (`.*`), `node_modules`, `vendor`, `__pycache__`, `venv`. `hl remove` adds an app's folder name here when the folder is in a projects folder. |
| `scan_depth` | `1` | How many levels below a projects folder to look. |
| `strip_prefix` | none | Folder-name prefixes left out of app names, by `hl init`, `hl sync` and `hl add` (without `name=`). A new homelab gets the prefix its folders share, if any. A folder loses the first one it starts with, unless nothing would be left. Apps already in the homelab keep their names. |

A folder with a `.hlignore` file in it is skipped too. Ignoring only stops sync from adding a
folder; an app already added stays. Apps outside the projects folders (added with `hl add`) are
never changed by sync, and an app whose projects folder is missing altogether (say, an unmounted
drive) is not removed.

`hl init` and `hl sync` only add **web apps** on their own: a folder whose service has none of the
preferred names (`frontend`, `web`, `app`, `nginx`, `proxy`, `ui`) and publishes a port that isn't
a common HTTP one (see [Built-in values](#built-in-values)) may be a database or a cache, which the
HTTP proxy can't serve. Such a folder is listed with the commands to use instead:
`hl add <folder> routing=none` to run it without the proxy, `hl add <folder>` to route it anyway,
or `hl set ignore+=<folder name>` to stop seeing it. `hl add` routes such a service anyway and
warns, naming `hl set <app> routing=none`.

### `name`

Sets this homelab apart from others on the same Docker; see
[Several homelabs on one Docker](#several-homelabs-on-one-docker-name).

## Variables hl provides

An app's `env` values (and its compose file, through them) can use these:

| Variable | Value |
| --- | --- |
| `HL_APP` | the app's name |
| `HL_URL` | the app's URL without a trailing slash, e.g. `http://localhost/my-app` (empty for `routing: none`) |
| `HL_PREFIX` | the path prefix, e.g. `/my-app`; empty unless routing is `path` |
| `HL_HOST` | the host name: `<app>.<domain>` for host routing, else `domain` |
| `HL_DATA` | the app's data folder, `<homelab>/data/<app>` |
| `TZ` | this machine's time zone (from `$TZ`, else what `/etc/localtime` points to, else `/etc/timezone`), so apps log in your local time - only when neither the shared env, the app's env nor the app's `.env` set `TZ`, and only when hl can tell the zone. `hl show` shows it and where it came from; `hl set env.TZ=UTC` picks another for every app. |

**Order:** a variable's value comes from, later winning: the app's `.env`,
your shell, the shared env, the app's env. `hl show <app>` lists the variables hl
provides and the env you set, each with where its value comes from (not the app's `.env`).

## Env hl fills in

`hl add`, `hl init` and `hl sync` ask `docker compose config --variables` which variables an
app's compose file uses, and set the ones that follow the common self-hosting conventions
(Immich, Paperless, linuxserver) as the app's `env`, saying what they mapped:

| Variable in the compose file | Gets | When |
| --- | --- | --- |
| `<X>_BASE_PATH` | `${HL_PREFIX}` | path routing |
| `<X>_URL`, with the same `<X>` as a `<X>_BASE_PATH` | `${HL_URL}` | path or host routing |
| `DATA_LOCATION` | `${HL_DATA}` | always |
| `BACKUP_LOCATION` | `${HL_DATA}/backups` | always |

Anything else is left alone: a `<X>_URL` without a matching `<X>_BASE_PATH` (say, the URL of an
upstream service) and `DB_DATA_LOCATION` and the like (named volumes are under `data/<app>/`
already). `hl add <folder> env.NAME=value` sets a value of your own (it wins over a mapped one),
`env.NAME=` with no value leaves that variable out, and `hl unset <app> env.NAME` drops one
later; `hl unset <app> env` drops them all.

## Several homelabs on one Docker (`name`)

| Key | Default | What it does |
| --- | --- | --- |
| `name` | none | Sets this homelab apart from others on the same Docker daemon (a second homelab of yours, or another user's on the same machine). Lowercase letters, digits and `-`, starting with a letter or digit. |

Compose project and network names are global to a Docker daemon. Without `name`, apps run as
`hl-<app>` and the proxy as `hl-proxy` on the network `hl-proxy`. With `name=lab2`, apps run as
`hl-lab2-<app>`, and the proxy's project and network are `hl-lab2-proxy`. Give the second
homelab its own proxy `port` too (two proxies can't both listen on port 80). `hl init` does both
for a new homelab when it finds another homelab's proxy on this Docker.

`name` renames every project, so `hl set` refuses it while any of this homelab's apps or its proxy
runs:

```sh
hl down
hl set name=lab2
hl set proxy port=8080
hl up
```

hl never starts, stops or replaces a compose project whose containers were started from another
homelab's folder (it reads compose's `config_files` label): it refuses and says how to fix it
(give this homelab a `name`, or stop the other one), `hl status` shows such an app as
`other homelab`, and `hl down` leaves it alone with a warning.

## The hl.yml file

You don't need to open it, but this is what `hl set` writes. Everything except an app's `path` is
optional:

```yaml
version: 1
# name: lab2               # only for a second homelab on the same Docker

projects: [~/code, ~/src]  # where hl init and hl sync look for apps
strip_prefix: [project__]

proxy:
  bind: 127.0.0.1          # 0.0.0.0 to reach the apps from your network
  port: 80
  routing: path            # default for apps: path or host
  domain: localhost
  # debug: true

env:                       # passed to every app; an app's own env wins
  TZ: UTC

apps:
  my-app:
    path: ~/code/my-app    # absolute, ~, or relative to the homelab folder
    service: web           # what the proxy forwards to (hl add picks it)
    port: 8080             # that service's port in its container (hl add picks it)
    env:
      MY_APP_BASE_PATH: ${HL_PREFIX}     # /my-app
      MY_APP_URL: ${HL_URL}              # http://localhost/my-app
```

- **Checked on every command.** Unknown keys (with a "did you mean"), wrong types, bad values and
  clashes between apps (same Docker project, URL prefix or host name) are all reported at once,
  each with its line and a fix, and the command stops. `hl doctor` lists them too.
- **`version: 1`** is the format. A file without it keeps working; the first time hl changes the
  file it adds `version: 1` and says so. A file with a newer version than your hl understands is
  refused with a message to update hl.
- **Broken by hand.** `hl set`/`hl unset` write only a valid result, so a broken `hl.yml` comes
  from editing it by hand. Every command then refuses with each problem's `hl.yml:line:column`
  and what is wrong; fix those lines in any editor (or `hl unset` a key that shouldn't be
  there), and `hl doctor` checks the file again.
- **Written whole.** `hl.yml`, the default-homelab note and every generated file are written to a
  temporary file and renamed into place, so a crash or Ctrl-C never leaves half a file. An
  existing file keeps its permissions, and a symlinked `hl.yml` stays a symlink.
- **Finding the homelab:** `--home`, then `$HL_HOME`, then the nearest folder at or above the
  current one that has an `hl.yml`, then the default `hl init` recorded.

## Built-in values

Every default above is defined once in hl's code (`internal/defaults`). A few more values are fixed
and not settings (some have a command-line flag):

| Value | Used for |
| --- | --- |
| `hl-` | compose project name prefix: `hl-<app>`, `hl-proxy`; with `name`, `hl-<name>-<app>`, `hl-<name>-proxy` |
| `.hl/` | generated files in the homelab: `.hl/<app>/override.yml` (and `builds.json`, the build settings its images were built with), `.hl/proxy/compose.yml`, `.hl/proxy/traefik.yml` |
| `data/` | app data in the homelab: `data/<app>/<volume>` |
| `web` | the Traefik entrypoint name (container port 80) |
| `traefik` on `:8080` | Traefik's entrypoint for `/ping` (the proxy's healthcheck); not published |
| `/etc/traefik/traefik.yml` | where the static config is mounted in the proxy |
| `ERROR`, `INFO` | Traefik's log level, and the one with `proxy.debug` |
| healthcheck every 10s, timeout 5s, 3 retries, start period 5s | the proxy container's healthcheck (`traefik healthcheck --ping`) |
| `unless-stopped` | the proxy container's restart policy |
| `200` | lines shown by `hl logs` and `hl logs proxy` (change with `--tail`) |
| `2m` | how long `--wait` waits for each app (change with `--wait-timeout`) |
| `20` | log lines `--wait` shows for each service that didn't become healthy |
| every `250ms`, `2s` each | how often `--wait` asks the proxy for a healthy app's URL until the app answers (anything but Traefik's own `404 page not found`), and how long one request may take |
| `8080`, `8000`, `8081`, `8888`, `8090` | the proxy ports `hl init` tries, in order, when the default one is in use (and error hints suggest) |
| `code`, `projects`, `src` | projects folders `hl init` suggests, under your home folder, when they exist |
| `homelab` | the folder `hl init` uses under your home folder when you name none |
| `.hlignore` | a folder holding a file with this name is never picked up as an app |
| `frontend`, `web`, `app`, `nginx`, `proxy`, `ui` | service names `hl add` prefers when several publish ports |
| `80`, `443`, `3000`, `3001`, `4000`, `4200`, `5000`, `5173`, `8000`, `8008`, `8080`, `8081`, `8088`, `8443`, `8888` | container ports `hl init` and `hl sync` take for a web app; a service with another port and none of the names above is not added by them |
| `<X>_BASE_PATH`, `<X>_URL`, `DATA_LOCATION`, `BACKUP_LOCATION` | the variables `hl add`, `init` and `sync` fill in ([Env hl fills in](#env-hl-fills-in)) |
| `<config dir>/hl/home` | where `hl init` records the default homelab (`~/.config` on Linux, `~/Library/Application Support` on macOS) |
| `bash`, else `sh` | the shell `hl shell` runs |
| GitHub releases API, `3s` | what `hl doctor` asks for the latest release, and how long it waits |

**Environment variables hl reads:** `HL_HOME` (the homelab folder), `NO_COLOR` (no colours;
colours are also off when output isn't a terminal).

## Errors and exit codes

An error is one `error:` line saying what failed, usually followed by a `hint:` line saying what
to do. When a docker command fails, hl reads its output to explain the common causes: the daemon
isn't running, your user may not use Docker, the port is taken, compose is too old, the subnet
overlaps another network, an image can't be pulled, the disk is full. Warnings (`warning: ...`)
don't stop the command. All of these go to stderr.

| Exit code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | A command failed: one of hl's own checks (including problems in `hl.yml`), or a docker or git command. |
| `2` | Usage: an unknown command, a wrong argument or flag. |
| `128 + signal` | Interrupted; `130` for Ctrl-C. |
| the shell's | `hl shell` exits with the status of the shell it ran. |

Commands that act on several apps carry on past one that fails and end with
`n of m apps failed: ...`.

**Ctrl-C** while docker runs: hl lets the docker command stop by itself (a Ctrl-C on the terminal
reaches it too; other signals are passed on), starts nothing more, and exits. A second Ctrl-C
stops it at once.
