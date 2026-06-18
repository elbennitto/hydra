"""MkDocs plugin: navigation generation and asciinema embeds on command pages."""

from __future__ import annotations

from functools import lru_cache
import html
from importlib import resources
import logging
import re
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from mkdocs.config.defaults import MkDocsConfig
    from mkdocs.livereload import LiveReloadServer

log = logging.getLogger("mkdocs.plugins.hydra_manual")

from mkdocs.config import config_options
from mkdocs.plugins import BasePlugin
from mkdocs.structure.files import File, InclusionLevel
from mkdocs.structure.pages import Page

# Paths in mkdocs.yml extra_css / extra_javascript (relative to docs_dir URLs).
# Physical files live next to mkdocs.yml; registered via on_files.
_SITE_ASSETS = (
    "assets/hydra-logo.svg",
    "assets/hydra-logo-borderless.svg",
    "stylesheets/extra.css",
    "javascripts/asciinema-embed.js",
)

_NPM_ASSETS = (
    (
        "assets/asciinema-player/asciinema-player.css",
        "node_modules/asciinema-player/dist/bundle/asciinema-player.css",
    ),
    (
        "assets/asciinema-player/asciinema-player.min.js",
        "node_modules/asciinema-player/dist/bundle/asciinema-player.min.js",
    ),
)

_COMMAND_H1 = re.compile(r"^#\s+hydra\s+(.+)$", re.MULTILINE)
_TREE_FENCE = re.compile(r"(?ms)^```tree[ \t]*\n(.*?)^```[ \t]*$")
_TREE_DIR_ICON_NAME = "material/folder-open"
_TREE_FILE_ICON_NAME = "material/file"
_SKIP_COMMAND_PAGES = frozenset(
    {
        "commands/README.md",
        "commands/inspect-shared.md",
    }
)

_SECTION_TITLES = {
    "appendix": "Appendix",
    "cel": "CEL",
    "ci": "CI",
    "configuration": "Configuration",
    "introduction": "Introduction",
    "migration": "Migration",
    "presets": "Presets",
    "refs": "Refs",
    "tutorials": "Tutorials",
    "values": "Values",
    "workflows": "Workflows",
    "concepts": "Concepts",
    "commands": "Commands",
    "argocd": "ArgoCD",
    "cluster": "GitOps (cluster)",
    "local": "Local",
}

# Top-level manual chapters after Home (CEL last).
_TOP_LEVEL_NAV_ORDER = (
    "tutorials",
    "commands",
    "appendix",
)


class HydraManualPlugin(BasePlugin):
    """Build nav from the manual tree and embed help recordings on command pages."""

    config_scheme = (
        ("asciinema_source", config_options.Type(str, default="../manual")),
        ("asciinema_site_path", config_options.Type(str, default="asciinema")),
    )

    def on_config(self, config) -> None:
        docs_dir = Path(config.docs_dir)
        if not config.nav:
            config.nav = _build_nav(docs_dir)

    def on_files(self, files, *, config):
        _exclude_record_specs(files)
        site_root = Path(config.config_file_path).parent.resolve()
        _register_static_files(files, config, site_root, _SITE_ASSETS)
        _register_npm_assets(files, config, site_root, _NPM_ASSETS)
        _register_asciinema_casts(files, config, site_root, self.config["asciinema_source"], self.config["asciinema_site_path"])
        return files

    def on_serve(
        self,
        server: "LiveReloadServer",
        /,
        *,
        config: "MkDocsConfig",
        builder,
    ) -> "LiveReloadServer":
        site_root = Path(config.config_file_path).parent.resolve()
        casts_dir = (site_root / self.config["asciinema_source"]).resolve()
        if casts_dir.is_dir():
            server.watch(str(casts_dir))
        for dirname in ("assets", "stylesheets", "javascripts", "hydra_mkdocs"):
            path = site_root / dirname
            if path.is_dir():
                server.watch(str(path))
        return server

    def on_page_markdown(self, markdown: str, *, page: Page, config, files) -> str:
        markdown = _render_tree_fences(markdown)
        if _has_asciinema_embed(markdown):
            return markdown
        rel = _page_relpath(page, config)
        cast_path = _command_cast_path(rel, markdown, self._casts_available(config))
        if cast_path is None:
            return markdown
        return _inject_asciinema_block(markdown, cast_path)

    def on_post_build(self, *, config, **kwargs) -> None:
        site_root = Path(config.config_file_path).parent
        site_dir = Path(config.site_dir)

        cname = site_root / "extra" / "CNAME"
        if cname.is_file():
            (site_dir / "CNAME").write_text(
                cname.read_text(encoding="utf-8").strip() + "\n",
                encoding="utf-8",
            )

    def _casts_available(self, config) -> set[str]:
        source = Path(config.config_file_path).parent / self.config["asciinema_source"]
        if not source.is_dir():
            return set()
        return {p.relative_to(source).as_posix() for p in source.rglob("*.cast")}


def _page_relpath(page: Page, config) -> str:
    docs_dir = Path(config.docs_dir).resolve()
    src = Path(page.file.abs_src_path).resolve()
    try:
        return src.relative_to(docs_dir).as_posix()
    except ValueError:
        return Path(page.file.src_path).as_posix()


def _register_static_files(files, config, site_root: Path, rel_paths: tuple[str, ...]) -> None:
    for src_uri in rel_paths:
        abs_path = site_root / src_uri
        if not abs_path.is_file():
            log.warning("site asset missing: %s", abs_path)
            continue
        if files.get_file_from_path(src_uri) is not None:
            continue
        files.append(
            File.generated(
                config,
                src_uri,
                abs_src_path=str(abs_path),
                inclusion=InclusionLevel.NOT_IN_NAV,
            )
        )


def _exclude_record_specs(files) -> None:
    for file in list(files):
        src_uri = getattr(file, "src_uri", "")
        if src_uri.endswith(".cast.yaml") or src_uri.endswith(".cast.yml"):
            files.remove(file)


def _register_npm_assets(files, config, site_root: Path, asset_map: tuple[tuple[str, str], ...]) -> None:
    for src_uri, package_path in asset_map:
        abs_path = site_root / package_path
        if not abs_path.is_file():
            log.warning("npm asset missing: %s", abs_path)
            continue
        if files.get_file_from_path(src_uri) is not None:
            continue
        files.append(
            File.generated(
                config,
                src_uri,
                abs_src_path=str(abs_path),
                inclusion=InclusionLevel.NOT_IN_NAV,
            )
        )


def _register_asciinema_casts(
    files,
    config,
    site_root: Path,
    asciinema_source: str,
    asciinema_site_path: str,
) -> None:
    casts_dir = site_root / asciinema_source
    if not casts_dir.is_dir():
        return
    prefix = asciinema_site_path.rstrip("/")
    for cast in sorted(casts_dir.rglob("*.cast")):
        rel = cast.relative_to(casts_dir).as_posix()
        src_uri = f"{prefix}/{rel}"
        if files.get_file_from_path(src_uri) is None:
            files.append(
                File.generated(
                    config,
                    src_uri,
                    abs_src_path=str(cast.resolve()),
                    inclusion=InclusionLevel.NOT_IN_NAV,
                )
            )


def _has_asciinema_embed(markdown: str) -> bool:
    return "hydra-asciinema" in markdown or "## CLI help recording" in markdown


def _command_cast_path(rel_path: str, markdown: str, casts: set[str]) -> str | None:
    if not rel_path.startswith("commands/") or not rel_path.endswith(".md"):
        return None
    if rel_path in _SKIP_COMMAND_PAGES:
        return None

    markdown_cast = rel_path[:-3] + ".cast"
    if markdown_cast in casts:
        return markdown_cast

    match = _COMMAND_H1.search(markdown)
    if not match:
        return None
    slug = match.group(1).strip().replace(" ", "-")
    legacy_help_cast = f"help/{slug}.cast"
    if legacy_help_cast not in casts:
        return None
    return legacy_help_cast


def _inject_asciinema_block(markdown: str, cast_path: str) -> str:
    block = (
        "\n\n## CLI help recording\n\n"
        f'<div class="hydra-asciinema" data-cast-path="{cast_path}"></div>\n\n'
    )
    synopsis = "\n## Synopsis\n"
    if synopsis in markdown:
        return markdown.replace(synopsis, block + synopsis, 1)
    lines = markdown.splitlines(keepends=True)
    for idx, line in enumerate(lines):
        if line.startswith("# "):
            return "".join(lines[: idx + 1]) + block + "".join(lines[idx + 1 :])
    return markdown + block


def _render_tree_fences(markdown: str) -> str:
    return _TREE_FENCE.sub(lambda match: _render_tree_block(match.group(1)), markdown)


def _render_tree_block(block: str) -> str:
    entries = _parse_tree_entries(block)
    if not entries:
        return ""
    root = _build_tree_nodes(entries)
    lines: list[str] = ['<div class="hydra-tree" role="img" aria-label="Directory tree">\n']
    _render_tree_nodes(root["children"], prefix_flags=[], lines=lines)
    lines.append("</div>")
    return "".join(lines)


def _parse_tree_entries(block: str) -> list[dict[str, object]]:
    entries: list[dict[str, object]] = []
    for raw_line in block.splitlines():
        if not raw_line.strip():
            continue
        line = raw_line.replace("\t", "    ")
        indent = len(line) - len(line.lstrip(" "))
        level = indent // 4
        name = line.strip()
        entries.append(
            {
                "level": level,
                "name": name,
                "is_dir": name.endswith("/"),
            }
        )
    return entries


def _build_tree_nodes(entries: list[dict[str, object]]) -> dict[str, object]:
    root: dict[str, object] = {"children": []}
    stack: list[dict[str, object]] = [root]
    for entry in entries:
        level = int(entry["level"])
        level = min(level, len(stack) - 1)
        while len(stack) - 1 > level:
            stack.pop()
        node = {
            "name": str(entry["name"]),
            "is_dir": bool(entry["is_dir"]),
            "children": [],
        }
        stack[-1]["children"].append(node)
        stack.append(node)
    return root


def _render_tree_nodes(
    nodes: list[dict[str, object]],
    *,
    prefix_flags: list[bool],
    lines: list[str],
) -> None:
    for index, node in enumerate(nodes):
        is_last = index == len(nodes) - 1
        branch_class = "hydra-tree__branch--root"
        if prefix_flags:
            branch_class = "hydra-tree__branch--el" if is_last else "hydra-tree__branch--tee"
        entry_class = "hydra-tree__entry--dir" if node["is_dir"] else "hydra-tree__entry--file"
        icon_svg = _load_theme_icon(_TREE_DIR_ICON_NAME if node["is_dir"] else _TREE_FILE_ICON_NAME)

        lines.append('<div class="hydra-tree__line">')
        for keep_pipe in prefix_flags:
            guide_class = "hydra-tree__guide--pipe" if keep_pipe else "hydra-tree__guide--empty"
            lines.append(f'<span class="hydra-tree__guide {guide_class}"></span>')
        lines.append(f'<span class="hydra-tree__branch {branch_class}"></span>')
        lines.append(
            f'<span class="hydra-tree__entry {entry_class}">'
            f'<span class="hydra-tree__icon">{icon_svg}</span>'
            f'<span class="hydra-tree__label">{html.escape(str(node["name"]))}</span>'
            f"</span>"
        )
        lines.append("</div>\n")

        children = node["children"]
        if children:
            _render_tree_nodes(children, prefix_flags=prefix_flags + [not is_last], lines=lines)


@lru_cache(maxsize=None)
def _load_theme_icon(icon_name: str) -> str:
    root = _material_icons_root()
    parts = icon_name.split("/")
    if len(parts) < 2:
        raise ValueError(f"invalid icon name: {icon_name!r}")
    icon_path = root.joinpath(*parts[:-1], f"{parts[-1]}.svg")
    try:
        return icon_path.read_text(encoding="utf-8").strip()
    except FileNotFoundError as exc:
        raise FileNotFoundError(f"theme icon not found: {icon_name}") from exc


@lru_cache(maxsize=1)
def _material_icons_root():
    candidates = (
        ("material.templates", (".icons",)),
        ("material", ("templates", ".icons")),
    )
    for package, rel_parts in candidates:
        try:
            root = resources.files(package).joinpath(*rel_parts)
        except ModuleNotFoundError:
            continue
        if root.is_dir():
            return root
    raise ModuleNotFoundError(
        "Material for MkDocs icon library not found. "
        "Install the theme package so icons can be loaded from its .icons directory."
    )


def _build_nav(docs_dir: Path) -> list:
    nav: list = [{"Home": "README.md"}]
    dirs_by_name = {
        child.name: child
        for child in docs_dir.iterdir()
        if child.is_dir() and not child.name.startswith(".")
    }
    ordered_names = list(_TOP_LEVEL_NAV_ORDER) + sorted(
        dirs_by_name.keys() - set(_TOP_LEVEL_NAV_ORDER)
    )
    for name in ordered_names:
        child = dirs_by_name.get(name)
        if child is None:
            continue
        section = _build_nav_dir(child, docs_dir)
        if section:
            title = _SECTION_TITLES.get(name, _title_case(name))
            nav.append({title: section})
    return nav


def _build_nav_dir(directory: Path, docs_root: Path) -> list:
    entries: list = []
    readme = directory / "README.md"
    if readme.is_file():
        entries.append({_page_title(readme): _rel_path(readme, docs_root)})

    for md in sorted(directory.glob("*.md")):
        if md.name == "README.md":
            continue
        entries.append({_page_title(md): _rel_path(md, docs_root)})

    for child in sorted(directory.iterdir(), key=lambda p: p.name):
        if not child.is_dir() or child.name.startswith("."):
            continue
        subsection = _build_nav_dir(child, docs_root)
        if subsection:
            title = _SECTION_TITLES.get(child.name, _title_case(child.name))
            entries.append({title: subsection})
    return entries


def _rel_path(path: Path, docs_root: Path) -> str:
    return path.resolve().relative_to(docs_root.resolve()).as_posix()


def _page_title(path: Path) -> str:
    try:
        text = path.read_text(encoding="utf-8")
    except OSError:
        return _title_case(path.stem)
    match = re.search(r"^#\s+(.+)$", text, re.MULTILINE)
    if match:
        title = match.group(1).strip()
        if title.lower().startswith("hydra "):
            return title
        return title
    return _title_case(path.stem)


def _title_case(name: str) -> str:
    return name.replace("-", " ").replace("_", " ").title()
