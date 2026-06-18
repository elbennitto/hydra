# Hydra user manual — static site

MkDocs Material site for [https://docs.hydra-gitops.org/](https://docs.hydra-gitops.org/).

| Path | Role |
| ---- | ---- |
| `../manual/` | Markdown source (not copied; read at build time) |
| `../manual/` | Markdown source plus terminal recordings (`.cast`, `*.cast.yaml`) |
| `hydra_mkdocs/` | MkDocs plugin (navigation, asciinema embeds) |
| `site/` | Build output (generated; do not edit) |

## Prerequisites

- **Python 3.10+** (`python3 --version`)
- **npm** (`npm --version`)
- Network access on first run (pip downloads MkDocs and dependencies)

## Quick test (build + static preview)

From the repository root:

```bash
cd hydra/docs/site
./build.sh
python3 -m http.server --directory site 8080
```

Open [http://127.0.0.1:8080/](http://127.0.0.1:8080/) in a browser.

**What to check**

- Home page loads and the left navigation lists all manual chapters.
- Open a command page, e.g. **Commands → Local → hydra local inspect**.
- Section **CLI help recording** shows an asciinema player; press play to run the cast.
- Workflow page **Workflows → Workflow: CI Pipeline** renders the Mermaid diagram.

Stop the preview server with `Ctrl+C`.

`./build.sh` creates a virtualenv in `.venv/`, installs Python and npm dependencies, and writes HTML to `site/`.

## Live reload while editing (recommended)

```bash
cd hydra/docs/site
./serve.sh
```

Opens [http://127.0.0.1:8000/](http://127.0.0.1:8000/) with live reload when you edit files under `../manual/`. Stop with `Ctrl+C`.

Changes to **asciinema casts** (`../manual/**/*.cast`) and **site assets** (player, `asciinema-embed.js`) trigger a rebuild automatically — you do not need to restart `./serve.sh` after `hydra record cli` or `hydra record file`. Use a hard refresh in the browser if a script change does not appear.

`./serve.sh` creates `.venv/` on first run, installs Python and npm dependencies, then runs `mkdocs serve`. Extra arguments are passed through, for example:

```bash
./serve.sh -a 127.0.0.1:9000
```

## Rebuild without recreating the venv

```bash
cd hydra/docs/site
source .venv/bin/activate
mkdocs build
python3 -m http.server --directory site 8080
```

## Clean rebuild

```bash
cd hydra/docs/site
rm -rf site/ .venv/
./build.sh
```

## Asciinema player

Installed via **npm** from `package.json` (`asciinema-player 3.15.1`). The MkDocs plugin serves the player bundle from `node_modules/asciinema-player/dist/bundle/`. Recordings use **asciicast v3** (`"version": 3` in `.cast` headers); the player must be **≥ 3.10.0** to play them.

Help recordings **autoplay** on load (`autoPlay: true`). `controls: true` keeps the **timeline scrubber** visible. While paused: `,` / `.` step frame-by-frame (`minFrameTime: 0`), arrow keys seek ±5s.

## Asciinema recordings

Command pages under `../manual/commands/` get a player when:

1. The page H1 is `# hydra <command path>` (e.g. `# hydra local inspect`), or the page embeds a player manually with `data-cast-path`.
2. A file exists under `../manual/`, for example `commands/local/inspect.cast` or `tutorials/introduction/no-context.cast`.

Record or refresh casts from the repo root with a built `hydra` binary:

```bash
hydra record cli
hydra record file hydra/docs/manual/tutorials/introduction/no-context.cast.yaml
# or multiple specs via shell expansion:
hydra record file hydra/docs/manual/tutorials/introduction/*.cast.yaml
```

Then rebuild or restart `mkdocs serve`.

## Production deploy

GitHub Actions workflow: `.github/workflows/publish.yml` (step `Build and publish manual`)
Publishes `hydra/docs/site/site/` to GitHub Pages (`gh-pages`) with custom domain (default `docs.hydra-gitops.org`).

Per-fork domain and site URL are configured in `.github/secrets/repos/<owner>/<repo>/publish.yaml` under `manual.pages_domain` and `manual.site_url`.
Optional overrides for target repository and target directory can be set via `manual.target_repo` and `manual.target_dir`.
Manual deploy uses an SSH deploy key loaded from `.github/secrets/repos/<owner>/<repo>/publish.sops.yaml` at `manual.pages_deploy_key` only when the publish target requires SSH authentication.

## Troubleshooting

| Problem | What to try |
| ------- | ----------- |
| `python3: command not found` | Install Python 3.10+ or use `python` if it points to 3.10+ |
| `pip install` fails | Upgrade pip: `pip install --upgrade pip` |
| `npm install` fails | Check Node/npm installation and registry access |
| Player empty / 404 on `.cast` | Run `mkdocs build` again; check `site/asciinema/commands/<group>/<command>.cast` exists |
| Old content in browser | Hard refresh (`Ctrl+Shift+R`) or use `mkdocs serve` instead of a stale `site/` |
| MkDocs warning about links | Many manual links target develop docs not included in this site yet; build still succeeds |
