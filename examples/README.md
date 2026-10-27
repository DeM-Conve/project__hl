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
hl up
hl status
```

`hl add examples/base-path-app` gives the app its prefix on its own: its compose file uses
`BASE_PATH_APP_BASE_PATH`, so hl maps it to `${HL_PREFIX}` and says so
(`env BASE_PATH_APP_BASE_PATH=${HL_PREFIX}`); `hl show base-path-app` lists it.

Then open `http://localhost/static-site/` and `http://localhost/base-path-app/`. To see what
breaks without the base path, `hl unset base-path-app env.BASE_PATH_APP_BASE_PATH` and
`hl up base-path-app`. Clean up with `hl down` and by deleting `~/homelab-demo`.
