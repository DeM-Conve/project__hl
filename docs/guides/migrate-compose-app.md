# Move an existing compose app into hl

You already run an app with `docker compose up` in its folder, with its data in Docker volumes.
This guide moves it behind hl without changing its compose file, and moves its data into the
homelab's `data/` folder.

What changes when hl runs the app:

| | Before | With hl |
| --- | --- | --- |
| Compose project | the folder's name, e.g. `my-app` | `hl-my-app` |
| URL | `http://localhost:8080` | `http://localhost/my-app/` (or `http://my-app.localhost`) |
| Host port | published by the app | none: only the proxy publishes a port |
| Named volumes | Docker volumes `my-app_<volume>` | folders `<homelab>/data/my-app/<volume>` |
| Bind mounts (`./uploads:/uploads`) | in the app's folder | unchanged, still in the app's folder |

Because the project name changes, hl starts the app with **new, empty volumes**. Copy the data
over first if you want to keep it.

## 1. Stop the app and note its volumes

```sh
cd ~/code/my-app
docker compose down                 # keeps the volumes
docker volume ls --filter label=com.docker.compose.project=my-app
```

## 2. Add it to hl

```sh
hl add ~/code/my-app
```

`hl add` prints the service and container port it will route to. If it picked the wrong one, run
`hl remove my-app` and add it again with `--service` and `--port`. If the app can't run under a
path prefix, add `--routing host` (see [base path](base-path-app.md)). Apps with nothing to route
(a worker, a database you reach directly) take `--routing none`; their published ports stay.

## 3. Copy the data

For each named volume, copy the old Docker volume into the folder hl will use. hl creates
`data/<app>/<volume>` on the first command that prepares the app (`hl config my-app` is enough):

```sh
hl config my-app > /dev/null        # creates data/my-app/<volume> folders
HOMELAB=$(hl path)
docker run --rm \
  -v my-app_db:/from:ro \
  -v "$HOMELAB/data/my-app/db":/to \
  alpine sh -c 'cp -a /from/. /to/'
```

Repeat with each volume name (`my-app_<volume>` -> `data/my-app/<volume>`). `cp -a` keeps owners
and permissions, which databases care about.

## 4. Start it through hl

```sh
hl up my-app
hl status
hl logs my-app
```

Check the app works at its new URL. Things that commonly need a setting now:

- **Its own URL** (for links in emails, OAuth redirect URIs, CSRF origin checks): set it from
  `${HL_URL}` in `hl.yml` (`env:`), and update redirect URIs registered with OAuth providers.
- **Base path**, with path routing: see [the base-path guide](base-path-app.md).

## 5. Clean up

When you're sure the data arrived, remove the old volumes:

```sh
docker volume rm my-app_db          # and the app's other old volumes
```

The app's folder is unchanged; `docker compose up` there still works on its own (with its own,
separate volumes), so don't run both at once.
