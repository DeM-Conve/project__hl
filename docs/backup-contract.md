# Backup contract, version 1

One way for a self-hosted app to take, list, verify and restore backups, so a plain `docker compose`
user, the app's own `scripts/backup.sh` / `scripts/restore.sh`, and `hl backup` / `hl restore` all drive
it the same way. Nothing in it is hl-specific: an app that follows it works without hl.

An app implements it with its own small `backup` image (Postgres apps: `pg_dump`/`psql`; SQLite apps:
`VACUUM INTO`), scheduled with supercronic.

## 1. Discovery

- The app's compose file has a service named **`backup`** with the label
  **`backup.contract: "1"`** (the contract version).
- The same service has the label **`backup.stop: "<service>[,<service>...]"`**: the services that write the
  data and must be stopped during a restore (e.g. `backend`). The database service is NOT in it.
- The `backup` service runs the schedule by default (supercronic + `BACKUP_SCHEDULE`) and has the
  host's `BACKUP_LOCATION` mounted at **`/backups`**.

A tool finds it with `docker compose config --format json` (service `backup`, its labels).

## 2. Commands

Every command runs as `docker compose run --rm --no-deps backup backup <command> ...`
(or `docker compose exec backup backup <command>` when the service is up). The executable inside
the image is **`backup`**.

| command | does | stdout |
|---|---|---|
| `backup create [--tag <tag>]` | takes a consistent backup while the app runs | last line: the archive's file name (relative to `/backups`) |
| `backup list` | lists the archives in `/backups`, newest first | one line per archive: `name<TAB>size_bytes<TAB>created_utc_iso8601<TAB>tag` (empty tag allowed) |
| `backup verify <name>` | checks the archive (format, checksums, the DB restores into a scratch copy / integrity check) | nothing on success |
| `backup restore <name\|latest>` | verifies, takes a `pre-restore` safety backup, swaps the data in, keeps the old data until the new data checks out, rolls back on failure | last line: the safety backup's file name |
| `backup run` | the default entrypoint: the scheduler (create + retention on `BACKUP_SCHEDULE`) | logs |

- Progress and messages go to **stderr**; stdout carries only what the table says.
- `restore` assumes the services in `backup.stop` are already stopped and the database service (if any)
  is running. Stopping and starting them is the caller's job (scripts/restore.sh or hl), see §5.
- When there is no data yet, `restore` takes no safety backup and prints nothing on stdout; a
  caller treats empty stdout after exit `0` as "no safety backup", not as an error.
- **`latest`** is the newest archive whose tag is not `pre-restore`: undoing a restore (going
  back to a safety backup) is always an explicit choice, by name.
- `restore` refuses an archive whose `app` differs, or whose `format` is newer than it understands.
- Exit codes: `0` ok, `1` failed, `2` usage error, `3` verify failed (archive bad, nothing changed),
  `4` restore failed and was rolled back (old data in place).
- `create`, retention and `restore` never run at the same time: each takes an exclusive lock on
  `/backups/.backup.lock` (`flock`) and waits for it. Otherwise a scheduled run during a restore can
  dump a half-swapped database, or retention can delete the archive being restored.
- A service that proxies to a `backup.stop` service (e.g. nginx in front of the backend) must look its
  address up again after a restart (nginx: `resolver 127.0.0.11` and `proxy_pass` through a variable),
  because a restarted container can come back with a new IP.

## 3. Archive

- Name: **`<app>-[<tag>-]YYYYmmdd-HHMMSS.tar.gz`** (UTC is not required; local time in `TZ`), directly in
  `BACKUP_LOCATION` (no daily/weekly subfolders). `<app>` is the app's fixed slug (e.g. `example.app`).
  Tags: `[a-z0-9-]+`; `pre-restore` is reserved for the safety backup.
- Contents:
  - `backup-info.txt` (the manifest), `key=value` lines:
    ```
    format=1
    app=example.app
    app_version=<version or git sha if known, else unknown>
    created=2026-11-23T03:30:00+05:30
    db=postgres            # postgres | sqlite
    schema_version=<latest applied Flyway version, or unknown>
    tag=
    sha256.db.sql=<hex>    # one sha256.<path> line per payload file
    ```
  - Payload: Postgres apps `db.sql` (plain `pg_dump --no-owner --no-privileges`); SQLite apps the
    snapshot file(s) by their own names (e.g. `user.db`, taken with `VACUUM INTO`).
- Not in a backup: `.env` and other secrets. An app whose data is encrypted with a key from `.env`
   says so in its README next to "Backup & restore".

## 4. Settings (env, with defaults; old names keep working as fallbacks)

`BACKUP_LOCATION` (./backups), `BACKUP_SCHEDULE` (@daily), `BACKUP_KEEP_DAYS` (7), `BACKUP_KEEP_WEEKS` (4),
`BACKUP_KEEP_MONTHS` (6), `TZ`, `PUID`/`PGID` (the backup files are owned by the host user).
Retention thins the flat folder to the newest per day / ISO week / month; tagged archives
(`pre-restore` and any `--tag`) are never deleted by retention.

## 5. Restore, end to end (what scripts/restore.sh and hl both do)

0. `latest` resolves to a name first: the first line of `backup list` whose tag is not
   `pre-restore` (§2), so the archive verified, confirmed and restored is the same one.
1. `backup verify <name>`; stop on failure.
2. Confirm with the user (skip with `--yes`).
3. `docker compose stop <backup.stop services>`.
4. `docker compose run --rm --no-deps backup backup restore <name>` (database service stays up).
5. `docker compose start <backup.stop services>`, wait for their healthchecks.
6. On a failure in 4 the old data is already back (exit 4); still do 5 and report.

## 6. Older backups

Each app's `backup restore` also accepts its own pre-contract files (e.g. the old `*.sql.gz` from
`postgres-backup-local` or an earlier `scripts/backup.sh`, including those under `daily/ weekly/ monthly/ last/`),
treating them as `format=0`. `backup list` shows them too, after the contract archives.

## 7. hl

- `hl backup [app...]` — `backup create` for the named apps, or every app that has the contract; apps
  without it are listed as skipped. `--tag <tag>`.
- `hl backup --list <app>` — `backup list`, formatted.
- `hl restore <app> <name|latest> [--yes]` — §5.
Nothing in hl.yml; everything comes from the compose labels.

## Out of scope (v1)

Off-site copies / encryption of archives, backing up `.env`, a shared base image across apps, restore across apps, an HTTP backup API in the apps.
