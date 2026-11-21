# hl

**Run your own Docker Compose apps behind one local proxy, from their own folders, without
touching their compose files.**

[![release](https://img.shields.io/github/v/release/DeM-Conve/project__hl?sort=semver)](https://github.com/DeM-Conve/project__hl/releases/latest)
[![license](https://img.shields.io/github/license/DeM-Conve/project__hl)](LICENSE)

You have a few projects, each with its own `compose.yml`, each wanting port 80 or 8080 and keeping
its data in some Docker volume. hl puts them all behind one [Traefik](https://traefik.io) proxy at
`http://localhost/<app>/`, keeps every app's data in one `data/` folder, and gives you one set of
commands. Each project stays an ordinary compose project that still runs on its own. hl is a small
CLI: no daemon, no database, no web UI.

```console
$ hl init ~/homelab
✓ docker is installed
...
hl init plan
  homelab    /home/you/homelab (new hl.yml, data/)
  projects   ~/code
  proxy      127.0.0.1:80 (this machine only), path routing: apps at http://localhost/<app>/
  + add   blog     ~/code/blog     web:8080
  + add   tracker  ~/code/tracker  frontend:80
                                   env TRACKER_BASE_PATH=${HL_PREFIX}
  ! skip  cache    ~/code/cache    not a web app? (service redis, port 6379) ...
Set it up like this? [Y/n]: y
...
start them: hl up blog tracker

$ hl up
==> proxy
 ✔ Container hl-proxy-traefik-1  Started
==> blog
 ✔ Container hl-blog-web-1  Started
...

$ hl status
proxy running  127.0.0.1:80  home /home/you/homelab

APP      STATE   HEALTH   URL                        FOLDER
blog     up 2/2  healthy  http://localhost/blog/     /home/you/code/blog
tracker  up 2/2  -        http://localhost/tracker/  /home/you/code/tracker
```

(Shortened; the `==>` sections are docker compose's own output.)

- [Quickstart](#quickstart) · [Install](#install) · [Everyday commands](#everyday-commands)
- [Changing settings](#changing-settings) · [Routing](#routing) · [Data folder](#data-folder)
- [Security](#security) · [Troubleshooting](#troubleshooting) · [Guides](#guides)
- [Uninstall](#uninstall) · [Contributing](#contributing) · [License](#license)
- Every setting and default: [docs/reference.md](docs/reference.md)

## Quickstart

Needs Linux or macOS (on Windows, run hl inside WSL 2) and Docker with the compose plugin v2.24
or newer.

```sh
curl -fsSL https://raw.githubusercontent.com/DeM-Conve/project__hl/main/install.sh | sh
hl doctor                 # checks Docker and compose

hl init ~/homelab         # finds the apps in ~/code, ~/projects or ~/src, shows a plan, asks once
hl up --wait              # starts the proxy, then every app; --wait waits for healthchecks
hl open my-app            # http://localhost/my-app/
```

`hl init ~/homelab --projects ~/work --yes` uses other folders and doesn't ask; `hl add
~/code/my-app` adds one app at a time. Folders named like `project__blog` become app `blog`
(`--strip-prefix none` keeps whole names). Run again on an existing homelab, `hl init` adds the
apps that are new in its projects folders and changes nothing else that its plan doesn't list.
No app of your own to try? Use the two in
[examples/](examples/).

If an app's page loads but its links, styles or redirects are broken, it needs its base path: see
[Routing](#routing).

## Install

hl runs on Linux and macOS (amd64 and arm64). On Windows it only prints an error saying so; run
it inside WSL 2 with Docker available there.

- **Install script** (verifies the download against the release's `checksums.txt`, installs to
  `~/.local/bin`, or `/usr/local/bin` as root):
  `curl -fsSL https://raw.githubusercontent.com/DeM-Conve/project__hl/main/install.sh | sh`.
  Add `-s -- --version v0.1.0 --dir /usr/local/bin` (or set `HL_VERSION`, `HL_INSTALL_DIR`) to
  pick a version or folder; sudo is used only if the folder isn't writable. With `--source`, or
  when there is no release yet, it builds hl from source instead: with Go if you have it,
  otherwise in a `golang` Docker container, so Docker is all a machine needs.
- **With Go** (1.21+): `go install github.com/DeM-Conve/project__hl/cmd/hl@latest`.
- **By hand:** download the archive from
  [Releases](https://github.com/DeM-Conve/project__hl/releases), check it
  (`sha256sum -c --ignore-missing checksums.txt`), and put `hl` on your PATH. Each release also
  carries an SPDX SBOM per archive.
- **Homebrew:** planned, not available yet.

Shell completion (app names, services, setting keys): `source <(hl completion bash)` (also `zsh`,
`fish`, `powershell`); add it to your shell's rc file.

## Everyday commands

Commands that take `[app...]` act on every app when none is named; `up`, `down`, `restart`,
`rebuild`, `logs` and `show` also take `proxy` for the proxy alone. Every command takes
`--home <folder>`; `hl <command> --help` lists its flags.

| Command | What it does |
| --- | --- |
| `hl init [folder]` | Set up a homelab folder (default `~/homelab`), find your apps, make it the default. Shows a plan and asks once; `--yes` doesn't ask. Flags: `--projects`, `--strip-prefix`; it picks the proxy itself (a free port), change it later with `hl set proxy`. |
| `hl add <folder> [key=value...]` | Add an app: any folder with a compose file. Takes the [settings](#changing-settings) `hl set <app>` takes, plus `name=`. |
| `hl up [app...]` | Start apps, the proxy first; with no app, also applies the proxy's changed settings (`hl up <app>` only starts a proxy that isn't running). Builds missing images, and rebuilds an image whose build args changed (a base path from `${HL_PREFIX}`, say). `--wait` waits (up to 2 minutes per app) until they're healthy and their URL answers through the proxy, and shows the logs of any that aren't. Ends with `hl status`. |
| `hl down [app...]` | Stop and remove containers; with no app, also the proxy. Data stays. |
| `hl restart [app...]` | Restart containers without rebuilding. An app that isn't running is left alone, with a note to `hl up` it. |
| `hl rebuild [app...]` | Rebuild images from the apps' current code, then restart. `--pull` first runs `git pull --ff-only` in each folder (a folder with uncommitted changes, or not a git repo, is rebuilt without a pull); for the proxy it pulls the image. |
| `hl logs [app] [service...]` | The last 200 lines (`-n`), `-f` to follow; with no app, every running app's, prefixed by its name. Says so when nothing is running. |
| `hl status` | The proxy, then each app's state, health, URL and folder. When the running proxy still listens where it was started (after `hl set proxy port=` without `hl up`), it shows that address, URLs on the port that serves them, and a note on applying hl.yml's. Says when `hl sync` has something to do. |
| `hl open <app>` | Print the app's URL and open it in the browser. |
| `hl shell <app> [service]` | A shell in a running container (default: the service behind the proxy). |
| `hl show [app\|proxy]` | Settings in effect and where each comes from; `--compose` prints the compose config as hl runs it. |
| `hl set` / `hl unset` | Change settings: see [Changing settings](#changing-settings). |
| `hl sync` | Make the homelab follow your projects folders: add new apps, remove ones whose folder is gone, follow a moved or renamed folder (the app keeps its name and data). Shows a plan and asks (n, or no terminal, only shows it); `--yes` applies it. |
| `hl remove <app>` | Stop the app and forget it. Its data stays; `hl sync` won't add its folder back. |
| `hl doctor` | Check Docker, compose, the settings, the proxy and every app; warns about an old Traefik or hl, and a running proxy started before hl.yml's last change. |
| `hl version` | Version, commit and platform, for bug reports. |
| `hl completion <shell>` | Shell completion script. |

## Changing settings

hl keeps its settings in `hl.yml` in the homelab folder; you don't need to open it. `hl set`
changes a setting, `hl unset` puts its default back, and `hl show [app|proxy]` prints what is in
effect and where each value comes from. The first argument says what to change:

```sh
hl set my-app routing=host               # an app (also on hl add <folder> routing=host)
hl set my-app env.TZ=Europe/Berlin       # a variable for that app's compose file
hl set proxy port=8080                   # the proxy
hl set env.TZ=UTC                        # env every app gets (an app's own wins)
hl set projects+=~/src                   # where hl init and hl sync look for apps
hl unset my-app routing                  # back to the default
```

hl writes the change only if the result is valid, then says which command applies it (most
often `hl up`). Every key with its default, lists (`+=`, `-=`) and renaming:
[how `hl set` works](docs/reference.md#how-hl-set-works).

## Routing

| `routing` | URL | Works with |
| --- | --- | --- |
| `path` (default) | `http://localhost/<app>/` | apps that can run under a base path |
| `host` | `http://<app>.localhost` | any app |
| `none` | not behind the proxy | databases, workers, apps that keep their own ports |

Set it per app (`hl set my-app routing=host`) or for every app (`hl set proxy routing=host`).
With path routing the app serves from `/`, but the links it builds must include `/<app>`, so it
needs its base-path setting, mapped from hl in its env (single quotes keep your shell from
expanding `${...}`):

```sh
hl set my-app 'env.MY_APP_BASE_PATH=${HL_PREFIX}' 'env.MY_APP_URL=${HL_URL}'
```

`hl add`, `hl init` and `hl sync` map the common ones (`<X>_BASE_PATH`, `<X>_URL`,
`DATA_LOCATION`, ...) on their own and say so. Details, and what host routing needs:
[routing](docs/reference.md#routing); walkthrough:
[an app that needs a base path](docs/guides/base-path-app.md).

## Data folder

The homelab folder holds `hl.yml`, `data/` (every app's data) and `.hl/` (files hl generates; safe
to delete). `hl init` writes a `.gitignore` for `data/` and `.hl/`, so you can keep `hl.yml` in git.

Every named volume an app declares becomes a plain folder, `data/<app>/<volume>`. Volumes marked
`external` or with a non-local driver are left as the app defines them. Bind mounts
(`./uploads:/uploads`) stay in the app's own folder, unless the app takes their location from a
variable (`BACKUP_LOCATION: ${HL_DATA}/backups`); hl creates those folders before it starts the app.

The app's folder is never edited: on every command hl writes `.hl/<app>/override.yml` and passes it
to `docker compose` after the app's own files. `hl remove` keeps `data/<app>`; delete it yourself
when you're sure.

**Backups:** there is no backup command. Stop the apps (`hl down`), copy the homelab folder with
ownership kept (`sudo rsync -a`), `hl up`. Guide: [back up the homelab](docs/guides/backup.md).

## Security

- The proxy listens on `127.0.0.1` (this machine only) unless you set `hl set proxy bind=0.0.0.0`,
  which exposes every app to your network with no authentication added by hl; see
  [reach your apps from your phone](docs/guides/lan-access.md).
- HTTP only: hl has no TLS. Put it [behind your own HTTPS proxy](docs/guides/behind-https.md) for
  an untrusted network. hl doesn't run the Traefik dashboard.
- The proxy mounts the Docker socket read-only; like any Docker tool, hl runs with your user's
  Docker rights.
- hl's only network request of its own is `hl doctor` asking GitHub for the latest release.

Report vulnerabilities privately: [SECURITY.md](SECURITY.md).

## Troubleshooting

Start with `hl doctor`. Every error says what failed and, on a `hint:` line, what to do.

- **`no homelab found`.** Run `hl init <folder>`, `cd` into a homelab folder, or set `HL_HOME` /
  `--home`.
- **Port 80 already in use.** hl names the program or container holding it. Stop it, or move the
  proxy: `hl set proxy port=8080`, `hl up`.
- **The page loads unstyled, or links and redirects lose the app's prefix.** The app doesn't know
  its base path: map `${HL_PREFIX}` ([Routing](#routing)) or `hl set <app> routing=host`.
- **404 from the proxy.** Check `hl status` and the URL (`/my-app/`). `hl set proxy debug=true`,
  `hl up proxy`, then `hl logs proxy -f` shows each request Traefik gets.
- **`hl.yml has 1 problem`** after editing it by hand. The error lists each problem as
  `hl.yml:line:column`; fix those lines in any editor (or `hl unset` a key that shouldn't be
  there, setting or not), then `hl doctor` checks it again. `hl set`/`hl unset` never break the file.
- **`hl add` can't tell which service to route to.** Pass `service=<name>` (and `port=<port>`).
- **`... belongs to another homelab`.** Two homelabs on one Docker: give one a
  [`name`](docs/reference.md#several-homelabs-on-one-docker-name), or stop the other.
- **Compose errors about `!reset`.** Your compose plugin is older than v2.24; update it.
- **Permission denied in `data/`.** Containers often own their files; use `sudo`, or the app's
  `PUID`/`PGID` if it has them.

Can an app still run without hl? Yes: `docker compose up` in its folder works as before, just not
at the same time as hl's copy.

## Guides

- [Add an app that needs a base path](docs/guides/base-path-app.md)
- [Move an existing compose app into hl](docs/guides/migrate-compose-app.md)
- [Reach your apps from your phone](docs/guides/lan-access.md)
- [Back up the homelab](docs/guides/backup.md)
- [Put hl behind HTTPS](docs/guides/behind-https.md)

hl is for one machine running your own projects. For a web UI over compose stacks, Dockge or
Portainer fit better; for push-to-deploy with domains and TLS, Dokku or Coolify; for several
servers, Komodo.

## Uninstall

1. `hl down` stops every app and the proxy.
2. `rm "$(command -v hl)"`.
3. Delete the default-homelab note: `~/.config/hl` (macOS: `~/Library/Application Support/hl`).
4. Optionally remove the images: `docker image rm traefik:v3.6.25` and the apps' images.

The homelab folder, with every app's data, stays until you delete it. App folders were never
changed.

## Contributing

Bug reports, fixes and small features are welcome: [CONTRIBUTING.md](CONTRIBUTING.md) (build,
checks, Conventional Commits) and the [Code of Conduct](CODE_OF_CONDUCT.md). Inside hl:
[docs/architecture.md](docs/architecture.md). Plans: [docs/product-plan.md](docs/product-plan.md).

## License

[MIT](LICENSE)
