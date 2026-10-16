# Security

Please don't report security problems in public issues.

Report them privately through GitHub:
[Security > Report a vulnerability](https://github.com/DeM-Conve/project__hl/security/advisories/new)
on this repository. Include what you found, how to reproduce it, and the output of `hl --version`.

The aim is to reply within a week. Fixes go into the latest release; older versions aren't patched.

hl runs `docker compose` with your user's rights and starts a Traefik proxy that listens on
`127.0.0.1` by default. Setting `proxy.bind: 0.0.0.0` exposes your apps to your network; that is
your choice, not a vulnerability in hl.
