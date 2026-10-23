# Security policy

## Supported versions

hl is young; only the latest release gets security fixes.

| Version | Supported |
| --- | --- |
| latest 0.x release | yes |
| older releases | no - please update |

## Reporting a vulnerability

Please don't report security problems in public issues.

Report them privately through GitHub:
[Security > Report a vulnerability](https://github.com/DeM-Conve/project__hl/security/advisories/new)
on this repository. Include what you found, how to reproduce it, and the output of `hl version`
and `hl doctor`.

The aim is to reply within a week and to publish a fix, with a GitHub security advisory, as soon
as it's ready. You'll be credited in the advisory unless you'd rather not be.

## hl's security model

- hl runs `docker compose` with your user's rights. Anyone who can run hl, or edit `hl.yml` and
  the apps' compose files, can do what Docker can on your machine.
- The proxy listens on `127.0.0.1` by default. `proxy.bind: 0.0.0.0` (or any non-loopback
  address) exposes every app to your network over plain HTTP, with no authentication added by hl.
  That is a deliberate setting, not a vulnerability in hl.
- The Traefik dashboard is **off by default** (`proxy.dashboard: false`). It has no login; hl
  refuses to start the proxy with it on a non-loopback bind unless `proxy.dashboard_public: true`
  is also set.
- The proxy container mounts the Docker socket read-only to discover apps. Read access to the
  socket still reveals every container's configuration, including environment variables.
- hl has no TLS in v0.1. To reach apps over an untrusted network, put hl behind your own HTTPS
  proxy ([guide](docs/guides/behind-https.md)).
- hl reads apps' `.env` files and the `env_files` you list, but never writes or copies secrets;
  `hl config <app>` prints the resolved config, including those values - mind where you paste it.
- Release archives come with `checksums.txt` (sha256), which `install.sh` checks, and an SPDX
  SBOM per archive.
