# Examples

Two tiny apps for trying hl, used by the [guides](../docs/guides/) and the end-to-end checks. Each
is an ordinary compose project that also runs on its own (`docker compose up`); hl doesn't need
anything inside them.

| Folder | What it shows |
|---|---|
| [static-site](static-site/) | An app with relative links: works under path routing with no settings. |
| [base-path-app](base-path-app/) | An app with absolute links: under path routing it needs its base path, fed from `${HL_PREFIX}`. |

Try both from a clone of this repository:

```sh
hl init ~/homelab-demo
hl add examples/static-site
hl add examples/base-path-app
```

Give `base-path-app` its prefix: in `~/homelab-demo/hl.yml`, add an `env` to the app hl just
added (keep the `path`, `service` and `port` lines it wrote):

```yaml
apps:
  base-path-app:
    # path, service, port: as hl add wrote them
    env:
      BASE_PATH_APP_BASE_PATH: ${HL_PREFIX}
```

```sh
hl up
hl status
```

Then open `http://localhost/static-site/` and `http://localhost/base-path-app/`. Clean up with
`hl down` and by deleting `~/homelab-demo`.
