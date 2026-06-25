#!/usr/bin/env python3
"""Build a single-file HTML prose-quality report from Vale + readability JSON."""

from __future__ import annotations

import argparse
import html
import json
import re
import sys
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import quote


def load_json(path: Path) -> object:
    with path.open(encoding="utf-8") as handle:
        return json.load(handle)


def rel_path(path: str, root: Path) -> str:
    try:
        return str(Path(path).resolve().relative_to(root.resolve()))
    except ValueError:
        return path


def read_vale_metadata(repo_root: Path) -> dict[str, str]:
    """Summarise language and checker config from .vale.ini for the report header."""
    ini_path = repo_root / ".vale.ini"
    packages = "Google, write-good"
    vocab = "Hydra"
    if ini_path.is_file():
        text = ini_path.read_text(encoding="utf-8")
        if match := re.search(r"^Packages\s*=\s*(.+)$", text, re.MULTILINE):
            packages = match.group(1).strip()
        if match := re.search(r"^Vocab\s*=\s*(.+)$", text, re.MULTILINE):
            vocab = match.group(1).strip()

    package_list = [part.strip() for part in packages.split(",") if part.strip()]
    return {
        "language": "English",
        "locale": "en-US",
        "locale_label": "US English",
        "spelling": "Vale spell-check with Google Developer Style (American English)",
        "style_guides": ", ".join(package_list + ["HydraReadability"]),
        "vocabulary": f"Custom accept list: {vocab}",
        "note": (
            "British English (en-GB) is not used. Flags like “don't” vs “do not” follow US style. "
            "Product names are allowed via the Hydra vocabulary file."
        ),
    }


def ease_label(score: float | None) -> str:
    if score is None:
        return "unknown"
    if score >= 60:
        return "plain English"
    if score >= 50:
        return "moderate (typical for tech docs)"
    if score >= 30:
        return "difficult"
    return "very difficult"


def grade_label(grade: float | None) -> str:
    if grade is None:
        return "unknown"
    return f"about school year {grade:.0f}"


def severity_rank(severity: str) -> int:
    order = {"error": 0, "warning": 1, "suggestion": 2}
    return order.get(severity.lower(), 3)


def snippet_for_line(lines: list[str], line_no: int, context: int = 1) -> str:
    if line_no < 1 or line_no > len(lines):
        return ""
    start = max(0, line_no - 1 - context)
    end = min(len(lines), line_no + context)
    parts: list[str] = []
    for idx in range(start, end):
        prefix = ">" if idx == line_no - 1 else " "
        parts.append(f"{prefix} {idx + 1:4d} | {lines[idx].rstrip()}")
    return "\n".join(parts)


def editor_uri(abs_path: Path, line: int, scheme: str) -> str:
    path = abs_path.resolve().as_posix()
    return f"{scheme}://file/{quote(path, safe='/:@')}:{line}:1"


def bucket_issues(hotspot: Counter, line_count: int, max_bars: int) -> list[dict]:
    """Group line-level issue counts into labelled buckets for charts."""
    if line_count <= 0:
        return []
    bar_count = min(max_bars, line_count)
    group_size = max(1, (line_count + bar_count - 1) // bar_count)
    buckets: list[dict] = []
    line = 1
    while line <= line_count:
        end = min(line_count, line + group_size - 1)
        count = sum(hotspot[row] for row in range(line, end + 1))
        peak_line = line
        peak_count = 0
        for row in range(line, end + 1):
            if hotspot[row] > peak_count:
                peak_count = hotspot[row]
                peak_line = row
        buckets.append(
            {
                "start": line,
                "end": end,
                "count": count,
                "peak_line": peak_line if peak_count else line,
            }
        )
        line = end + 1
    return buckets


def render_chart(
    hotspot: Counter,
    line_count: int,
    max_hot: int,
    anchor_prefix: str,
    *,
    mini: bool = False,
) -> str:
    if line_count <= 0 or not hotspot:
        return '<p class="muted">No line-level issues to chart.</p>'

    max_bars = 20 if mini else 36
    buckets = bucket_issues(hotspot, line_count, max_bars)
    chart_max = max((bucket["count"] for bucket in buckets), default=0) or 1
    y_ticks = [chart_max, chart_max // 2, 0] if chart_max > 1 else [1, 0]

    parts = [
        '<div class="chart">',
        '<div class="chart-header">',
        '<div class="chart-title">Writing issues by line in the file</div>',
    ]
    if mini:
        parts.append('<div class="chart-caption">Preview — open file detail for full chart.</div>')
    else:
        parts.append(
            '<div class="chart-caption">'
            "Each bar is a <strong>group of neighbouring lines</strong>. "
            "Bar <strong>height</strong> = how many writing-check flags fell in that group. "
            "Click a bar to jump to the busiest line in that group."
            "</div>"
        )
    parts.append("</div>")
    parts.append('<div class="chart-body">')
    parts.append('<div class="y-axis-title">Issue count</div>')
    parts.append('<div class="chart-plot">')
    parts.append('<div class="y-axis-ticks">')
    for tick in y_ticks:
        parts.append(f'<span>{tick}</span>')
    parts.append("</div>")
    parts.append('<div class="chart-bars" role="img">')
    for bucket in buckets:
        height = int(100 * bucket["count"] / chart_max) if bucket["count"] else 0
        label = (
            f"Lines {bucket['start']}–{bucket['end']}: {bucket['count']} issue(s)"
            if bucket["start"] != bucket["end"]
            else f"Line {bucket['start']}: {bucket['count']} issue(s)"
        )
        jump = f"#{anchor_prefix}-line-{bucket['peak_line']}"
        parts.append(
            f'<a class="chart-bar-wrap" href="{jump}" title="{html.escape(label)}">'
            f'<span class="chart-bar" style="height:{max(height, 4 if bucket["count"] else 0)}%"></span>'
            f"</a>"
        )
    parts.append("</div></div>")
    parts.append('<div class="x-axis">')
    parts.append(f'<span>Line 1</span><span class="x-mid">Line {line_count // 2}</span><span>Line {line_count}</span>')
    parts.append("</div>")
    parts.append('<div class="x-axis-title">Line number in document</div>')
    parts.append("</div></div>")
    return f'<div class="screen-only">{"".join(parts)}</div>'


def render_print_hotspot_table(hotspot: Counter, *, limit: int = 12) -> str:
    if not hotspot:
        return ""
    rows = hotspot.most_common(limit)
    parts = [
        '<table class="print-hotspot-table">',
        "<thead><tr><th>Line</th><th>Issues</th></tr></thead><tbody>",
    ]
    for line_no, count in rows:
        parts.append(f"<tr><td>{line_no}</td><td>{count}</td></tr>")
    parts.append("</tbody></table>")
    return "".join(parts)


def build_report(
    vale_data: dict[str, list[dict]],
    read_data: list[dict],
    repo_root: Path,
    target_label: str,
    meta: dict[str, str],
) -> str:
    read_by_file = {rel_path(item["file"], repo_root): item for item in read_data}

    files = sorted(set(vale_data.keys()) | set(read_by_file.keys()))
    file_models: list[dict] = []

    total_errors = 0
    total_warnings = 0
    total_suggestions = 0

    for file_path in files:
        rel = rel_path(file_path, repo_root) if file_path.startswith("/") else file_path
        abs_path = (repo_root / rel).resolve()
        source_lines = abs_path.read_text(encoding="utf-8").splitlines() if abs_path.is_file() else []

        alerts = vale_data.get(file_path, vale_data.get(rel, []))
        sev_counts = Counter(a.get("Severity", "suggestion").lower() for a in alerts)
        total_errors += sev_counts.get("error", 0)
        total_warnings += sev_counts.get("warning", 0)
        total_suggestions += sev_counts.get("suggestion", 0)

        by_line: dict[int, list[dict]] = defaultdict(list)
        for alert in alerts:
            by_line[int(alert.get("Line", 0))].append(alert)

        hotspot = Counter(int(a.get("Line", 0)) for a in alerts)
        max_hot = max(hotspot.values()) if hotspot else 0

        read = read_by_file.get(rel, {})
        readability = read.get("readability", {})
        structural = read.get("structural", {})

        file_models.append(
            {
                "rel": rel,
                "abs_path": abs_path,
                "anchor": slug(rel),
                "alerts": alerts,
                "sev_counts": sev_counts,
                "by_line": dict(sorted(by_line.items())),
                "hotspot": hotspot,
                "max_hot": max_hot,
                "line_count": len(source_lines),
                "source_lines": source_lines,
                "read": read,
                "fk": readability.get("flesch_kincaid_grade"),
                "ari": readability.get("ari"),
                "ease": readability.get("flesch_reading_ease"),
                "words": structural.get("words"),
                "status": read.get("status", "n/a"),
            }
        )

    generated = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")

    parts: list[str] = [
        "<!DOCTYPE html>",
        '<html lang="en">',
        "<head>",
        '<meta charset="utf-8">',
        '<meta name="viewport" content="width=device-width, initial-scale=1">',
        "<title>Hydra docs prose quality report</title>",
        "<script>",
        "if (new URLSearchParams(location.search).get('pdf') === '1') {",
        "  document.addEventListener('DOMContentLoaded', function () {",
        "    document.querySelectorAll('details.line-issue').forEach(function (el) { el.open = true; });",
        "  });",
        "}",
        "</script>",
        "<style>",
        css(),
        "</style>",
        "</head>",
        "<body>",
        '<header class="hero">',
        '<div class="hero-top">',
        "<h1>Docs prose quality report</h1>",
        '<div class="toolbar no-print">',
        '<button type="button" class="btn" onclick="exportPdf()" title="Opens print dialog. Enable “Background graphics” if colours or charts are missing.">Export to PDF</button>',
        "</div>",
        "</div>",
        f'<p class="meta">Target: <code>{html.escape(target_label)}</code> · Generated {generated}</p>',
        "</header>",
        '<section class="locale panel">',
        "<h2>Language and checkers</h2>",
        '<dl class="locale-grid screen-only">',
        f"<dt>Document language</dt><dd>{html.escape(meta['language'])} ({html.escape(meta['locale'])}) — {html.escape(meta['locale_label'])}</dd>",
        f"<dt>Spelling</dt><dd>{html.escape(meta['spelling'])}</dd>",
        f"<dt>Style guides</dt><dd>{html.escape(meta['style_guides'])}</dd>",
        f"<dt>Allowed terms</dt><dd>{html.escape(meta['vocabulary'])}</dd>",
        "</dl>",
        f'<p class="hint screen-only">{html.escape(meta["note"])}</p>',
        '<p class="hint print-only">British English is not used. US spelling and style (Google Developer Style).</p>',
        '<table class="print-only locale-print-table">',
        "<tbody>",
        f"<tr><th>Language</th><td>{html.escape(meta['language'])} ({html.escape(meta['locale'])}) — {html.escape(meta['locale_label'])}</td></tr>",
        f"<tr><th>Spelling</th><td>{html.escape(meta['spelling'])}</td></tr>",
        f"<tr><th>Style guides</th><td>{html.escape(meta['style_guides'])}</td></tr>",
        f"<tr><th>Vocabulary</th><td>{html.escape(meta['vocabulary'])}</td></tr>",
        "</tbody></table>",
        "</section>",
        '<section class="cards screen-only">',
        card("Files", str(len(file_models)), ""),
        card("Errors", str(total_errors), "sev-error"),
        card("Warnings", str(total_warnings), "sev-warning"),
        card("Suggestions", str(total_suggestions), "sev-suggestion"),
        "</section>",
        '<table class="print-only summary-print-table"><tbody><tr>',
        f"<th>Files</th><td>{len(file_models)}</td>",
        f"<th>Errors</th><td>{total_errors}</td>",
        f"<th>Warnings</th><td>{total_warnings}</td>",
        f"<th>Suggestions</th><td>{total_suggestions}</td>",
        "</tr></tbody></table>",
        '<section class="explain panel screen-only">',
        "<h2>How to read this report</h2>",
        "<ul>",
        "<li><strong>Line hotspot chart</strong> — bar chart by line number; taller bar = more flags in that line range.</li>",
        "<li><strong>Open in editor</strong> — jump to the exact line in Cursor/VS Code to fix text.</li>",
        "<li><strong>Reading level</strong> — school-year estimate; technical words push it up.</li>",
        "<li><strong>Ease score</strong> — 0–100, higher is easier; 50–65 is normal for tutorials.</li>",
        "</ul>",
        "</section>",
        '<section class="files panel">',
        "<h2>Files</h2>",
        '<table class="file-table">',
        "<thead><tr>",
        "<th class=\"file-cell\">File</th><th class=\"issues-cell\">Issues</th>"
        "<th class=\"level-cell\">Reading level</th><th class=\"ease-cell\">Ease</th>"
        '<th class="mini-heat screen-only">Hotspot preview</th>',
        "</tr></thead><tbody>",
    ]

    for model in sorted(file_models, key=lambda m: sum(m["sev_counts"].values()), reverse=True):
        issue_total = sum(model["sev_counts"].values())
        parts.append("<tr>")
        parts.append(
            f'<td class="file-cell"><a href="#{model["anchor"]}" title="{html.escape(model["rel"])}">'
            f'<span class="file-basename">{html.escape(Path(model["rel"]).name)}</span>'
            f'<span class="file-dir print-only">{html.escape(model["rel"])}</span>'
            f"</a></td>"
        )
        parts.append(
            f'<td class="issues-cell">'
            f'<span class="screen-only">'
            f'{badge("error", model["sev_counts"].get("error", 0))}'
            f'{badge("warning", model["sev_counts"].get("warning", 0))}'
            f'{badge("suggestion", model["sev_counts"].get("suggestion", 0))}'
            f"</span>"
            f'<span class="print-only issue-count">{issue_total} '
            f'({model["sev_counts"].get("error", 0)}E / '
            f'{model["sev_counts"].get("warning", 0)}W / '
            f'{model["sev_counts"].get("suggestion", 0)}S)</span>'
            f"</td>"
        )
        parts.append(f'<td class="level-cell">{html.escape(grade_label(model["fk"]))}</td>')
        ease = model["ease"]
        parts.append(
            f'<td class="ease-cell">'
            f'<span class="screen-only">{ease:.0f} — {html.escape(ease_label(ease))}</span>'
            f'<span class="print-only">{ease:.0f}</span>'
            f"</td>" if ease is not None else "<td>—</td>"
        )
        parts.append(
            f'<td class="mini-heat screen-only">{render_chart(model["hotspot"], model["line_count"], model["max_hot"], model["anchor"], mini=True)}</td>'
        )
        parts.append("</tr>")

    parts.extend(["</tbody></table>", "</section>", '<section class="detail panel">', "<h2>File detail</h2>"])

    for model in file_models:
        parts.append(f'<article class="file-block" id="{model["anchor"]}">')
        parts.append(f"<h3>{html.escape(model['rel'])}</h3>")
        parts.append(
            f'<p class="file-path screen-only"><code>{html.escape(str(model["abs_path"]))}</code></p>'
        )
        parts.append(f'<p class="file-path print-only"><code>{html.escape(model["rel"])}</code></p>')

        parts.append('<div class="metrics">')
        if model["fk"] is not None:
            parts.append(f"<span><strong>Reading level:</strong> {html.escape(grade_label(model['fk']))}</span>")
        if model["ease"] is not None:
            parts.append(
                f"<span><strong>Ease:</strong> {model['ease']:.0f} ({html.escape(ease_label(model['ease']))})</span>"
            )
        if model["words"] is not None:
            parts.append(f"<span><strong>Words:</strong> {model['words']}</span>")
        parts.append(f"<span><strong>Metrics status:</strong> {html.escape(str(model['status']))}</span>")
        parts.append("</div>")

        if model["hotspot"]:
            parts.append(render_chart(
                model["hotspot"], model["line_count"], model["max_hot"], model["anchor"], mini=False
            ))
            parts.append('<div class="print-only">')
            parts.append("<h4>Busiest lines (issue count)</h4>")
            parts.append(render_print_hotspot_table(model["hotspot"]))
            parts.append("</div>")

        parts.append("<h4>Issues by line</h4>")
        if not model["by_line"]:
            parts.append("<p class='ok'>No Vale issues on this file.</p>")
        else:
            parts.append('<div class="issues">')
            for line_no, alerts in model["by_line"].items():
                alerts_sorted = sorted(alerts, key=lambda a: severity_rank(a.get("Severity", "")))
                line_id = f'{model["anchor"]}-line-{line_no}'
                cursor_href = editor_uri(model["abs_path"], line_no, "cursor")
                vscode_href = editor_uri(model["abs_path"], line_no, "vscode")
                file_href = model["abs_path"].resolve().as_uri() + f"#line-{line_no}"

                parts.append(f'<details class="line-issue" id="{line_id}" open>')
                parts.append(
                    f"<summary>"
                    f"<span class='line-label'>Line {line_no}</span> — {len(alerts_sorted)} issue(s) "
                    f'{badge("error", sum(1 for a in alerts_sorted if a.get("Severity","").lower()=="error"))}'
                    f'{badge("warning", sum(1 for a in alerts_sorted if a.get("Severity","").lower()=="warning"))}'
                    f'{badge("suggestion", sum(1 for a in alerts_sorted if a.get("Severity","").lower()=="suggestion"))}'
                    f"</summary>"
                )
                parts.append('<div class="edit-links no-print">')
                parts.append(f'<a class="edit-link primary" href="{html.escape(cursor_href)}">Open line {line_no} in Cursor</a>')
                parts.append(f'<a class="edit-link" href="{html.escape(vscode_href)}">VS Code</a>')
                parts.append(f'<a class="edit-link" href="{html.escape(file_href)}">{html.escape(model["rel"])}:{line_no}</a>')
                parts.append("</div>")
                snippet = snippet_for_line(model["source_lines"], line_no)
                if snippet:
                    parts.append(f"<pre class='snippet'>{html.escape(snippet)}</pre>")
                parts.append("<ul>")
                for alert in alerts_sorted:
                    sev = alert.get("Severity", "suggestion").lower()
                    check = alert.get("Check", "")
                    message = alert.get("Message", "")
                    match = alert.get("Match", "")
                    parts.append(
                        f'<li class="sev-{html.escape(sev)}">'
                        f"<strong>{html.escape(sev)}</strong> "
                        f"<code>{html.escape(check)}</code>: {html.escape(message)}"
                    )
                    if match:
                        parts.append(f' <em>match: {html.escape(match)}</em>')
                    parts.append("</li>")
                parts.append("</ul></details>")
            parts.append("</div>")

        parts.append("</article>")

    parts.extend([
        "</section>",
        "<script>",
        js(),
        "</script>",
        "</body>",
        "</html>",
    ])
    return "\n".join(parts)


def slug(path: str) -> str:
    return "".join(ch if ch.isalnum() else "-" for ch in path).strip("-")


def card(title: str, value: str, extra_class: str) -> str:
    cls = f"card {extra_class}".strip()
    return f'<div class="{cls}"><div class="card-label">{html.escape(title)}</div><div class="card-value">{html.escape(value)}</div></div>'


def badge(severity: str, count: int) -> str:
    if count == 0:
        return ""
    return f'<span class="badge sev-{severity}">{count} {severity}</span>'


def js() -> str:
    return """
function preparePrintView() {
  document.querySelectorAll('details.line-issue').forEach(function (el) { el.open = true; });
}

function exportPdf() {
  preparePrintView();
  window.print();
}

if (new URLSearchParams(window.location.search).get('pdf') === '1') {
  document.addEventListener('DOMContentLoaded', preparePrintView);
}
"""


def css() -> str:
    return """
:root {
  --bg: #0f1419;
  --panel: #1a2332;
  --text: #e6edf3;
  --muted: #9da7b3;
  --error: #ff6b6b;
  --warning: #ffb020;
  --suggestion: #4dabf7;
  --accent: #63e6be;
  --border: #2d3a4f;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  font-family: system-ui, -apple-system, Segoe UI, sans-serif;
  background: var(--bg);
  color: var(--text);
  line-height: 1.5;
}
.hero, .cards, .locale, .explain, .files, .detail {
  max-width: 1100px;
  margin: 0 auto;
  padding: 1rem 1.25rem;
}
.hero-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}
.hero h1 { margin: 0 0 0.25rem; }
.meta { color: var(--muted); margin: 0; }
.toolbar { display: flex; gap: 0.5rem; flex-shrink: 0; }
.btn {
  background: var(--accent);
  color: #0f1419;
  border: none;
  border-radius: 8px;
  padding: 0.55rem 0.9rem;
  font-weight: 600;
  cursor: pointer;
}
.btn:hover { filter: brightness(1.05); }
.panel {
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 12px;
  margin-top: 1rem;
}
.locale h2, .explain h2, .files h2, .detail h2 {
  margin-top: 0;
  font-size: 1.1rem;
}
.locale-grid {
  display: grid;
  grid-template-columns: 10rem 1fr;
  gap: 0.35rem 1rem;
  margin: 0;
}
.locale-grid dt { color: var(--muted); font-weight: 600; }
.locale-grid dd { margin: 0; }
.cards {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 0.75rem;
  margin-top: 1rem;
}
@media (max-width: 720px) {
  .cards { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
.card {
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 0.9rem 1rem;
}
.card-label { color: var(--muted); font-size: 0.85rem; }
.card-value { font-size: 1.8rem; font-weight: 700; }
.card.sev-error .card-value { color: var(--error); }
.card.sev-warning .card-value { color: var(--warning); }
.card.sev-suggestion .card-value { color: var(--suggestion); }
.file-table { width: 100%; border-collapse: collapse; table-layout: fixed; }
.file-table th, .file-table td {
  border-bottom: 1px solid var(--border);
  padding: 0.75rem 0.5rem;
  text-align: left;
  vertical-align: top;
  overflow-wrap: anywhere;
  word-break: break-word;
}
.file-table .file-cell { width: 22%; }
.file-table .issues-cell { width: 18%; white-space: nowrap; }
.file-table .level-cell { width: 18%; }
.file-table .ease-cell { width: 22%; }
.file-table .mini-heat { width: 20%; }
.file-basename { display: block; font-weight: 500; }
.file-cell a { color: var(--accent); text-decoration: none; }
.file-cell a:hover .file-basename { text-decoration: underline; }
.badge {
  display: inline-block;
  margin-right: 0.25rem;
  padding: 0.1rem 0.45rem;
  border-radius: 999px;
  font-size: 0.75rem;
}
.badge.sev-error { background: #3b1212; color: var(--error); }
.badge.sev-warning { background: #3b2a12; color: var(--warning); }
.badge.sev-suggestion { background: #122b3b; color: var(--suggestion); }
.mini-heat { min-width: 200px; max-width: 260px; }
.mini-heat .chart-caption { display: none; }
.mini-heat .y-axis-title { display: none; }
.mini-heat .chart { margin: 0; }
.chart { margin: 0.5rem 0; }
.chart-header { margin-bottom: 0.35rem; }
.chart-title { font-weight: 700; font-size: 0.95rem; }
.chart-caption { color: var(--muted); font-size: 0.85rem; margin-top: 0.15rem; }
.chart-body {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 0.25rem 0.5rem;
  align-items: stretch;
}
.y-axis-title {
  writing-mode: vertical-rl;
  transform: rotate(180deg);
  text-align: center;
  color: var(--muted);
  font-size: 0.8rem;
  padding-right: 0.15rem;
}
.chart-plot {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 0.35rem;
  min-height: 120px;
}
.y-axis-ticks {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  color: var(--muted);
  font-size: 0.75rem;
  padding: 0.1rem 0;
}
.chart-bars {
  display: flex;
  align-items: flex-end;
  gap: 3px;
  border-left: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
  padding: 0.25rem 0.15rem 0;
  min-height: 110px;
  background: linear-gradient(to top, rgba(255,255,255,0.03) 1px, transparent 1px) 0 0 / 100% 33.33%;
}
.chart-bar-wrap {
  flex: 1;
  display: flex;
  align-items: flex-end;
  min-width: 4px;
  height: 100px;
  text-decoration: none;
}
.chart-bar {
  width: 100%;
  background: linear-gradient(to top, #c92a2a, #ffb020);
  border-radius: 3px 3px 0 0;
  min-height: 0;
}
.chart-bar-wrap:hover .chart-bar { filter: brightness(1.15); }
.x-axis {
  grid-column: 2;
  display: flex;
  justify-content: space-between;
  color: var(--muted);
  font-size: 0.75rem;
  padding: 0.1rem 0.15rem 0;
}
.x-axis-title {
  grid-column: 2;
  text-align: center;
  color: var(--muted);
  font-size: 0.8rem;
}
.file-block {
  border-top: 1px solid var(--border);
  padding: 1rem 0;
}
.file-block h3 { margin-top: 0; margin-bottom: 0.25rem; }
.file-path { margin: 0 0 0.75rem; color: var(--muted); font-size: 0.85rem; }
.metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem 1.25rem;
  color: var(--muted);
  margin-bottom: 0.75rem;
}
.hint { color: var(--muted); font-size: 0.85rem; }
.line-issue {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 0.35rem 0.65rem;
  margin-bottom: 0.5rem;
  background: #121a24;
  scroll-margin-top: 1rem;
}
.line-issue summary { cursor: pointer; font-weight: 600; }
.line-label { color: var(--accent); }
.edit-links {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin: 0.35rem 0 0.5rem;
}
.edit-link {
  display: inline-block;
  padding: 0.25rem 0.55rem;
  border-radius: 6px;
  border: 1px solid var(--border);
  color: var(--accent);
  text-decoration: none;
  font-size: 0.85rem;
}
.edit-link.primary {
  background: #12352b;
  border-color: #1f5c49;
}
.snippet {
  background: #0b1017;
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 0.5rem 0.75rem;
  overflow-x: auto;
  font-size: 0.85rem;
}
.line-issue ul { margin: 0.35rem 0 0.25rem 1rem; }
.sev-error { color: var(--error); }
.sev-warning { color: var(--warning); }
.sev-suggestion { color: var(--suggestion); }
.ok { color: var(--accent); }
.muted { color: var(--muted); }
.print-only { display: none !important; }

@media print {
  @page {
    size: A4 portrait;
    margin: 10mm;
  }
  * {
    -webkit-print-color-adjust: exact !important;
    print-color-adjust: exact !important;
  }
  html { font-size: 10pt; }
  body {
    background: var(--bg) !important;
    color: var(--text) !important;
  }
  .no-print, .print-only { display: none !important; }

  .hero, .cards, .locale, .explain, .files, .detail {
    max-width: none;
    padding: 0.5rem 0;
  }
  .panel, .card, .line-issue, .chart, .file-table {
    -webkit-print-color-adjust: exact !important;
    print-color-adjust: exact !important;
  }
  .cards {
    display: grid !important;
    grid-template-columns: repeat(4, minmax(0, 1fr)) !important;
    gap: 0.5rem;
    break-inside: avoid;
    page-break-inside: avoid;
  }
  .card {
    background: var(--panel) !important;
    border: 1px solid var(--border) !important;
    border-radius: 8px;
  }
  .card.sev-error .card-value { color: var(--error) !important; }
  .card.sev-warning .card-value { color: var(--warning) !important; }
  .card.sev-suggestion .card-value { color: var(--suggestion) !important; }
  .badge.sev-error { background: #3b1212 !important; color: var(--error) !important; }
  .badge.sev-warning { background: #3b2a12 !important; color: var(--warning) !important; }
  .badge.sev-suggestion { background: #122b3b !important; color: var(--suggestion) !important; }
  .chart-bar {
    background: linear-gradient(to top, #c92a2a, #ffb020) !important;
  }
  .file-table {
    break-inside: avoid;
    page-break-inside: avoid;
  }
  .mini-heat .chart {
    break-inside: avoid;
  }
  .file-block {
    break-before: page;
    page-break-before: always;
    padding-top: 0.75rem;
  }
  .file-block:first-child {
    break-before: auto;
    page-break-before: auto;
  }
  .line-issue {
    break-inside: avoid;
    page-break-inside: avoid;
    background: #121a24 !important;
  }
  .snippet {
    background: #0b1017 !important;
    border: 1px solid var(--border) !important;
    white-space: pre-wrap;
    word-break: break-word;
  }
  details.line-issue summary {
    list-style: none;
  }
  details.line-issue summary::-webkit-details-marker {
    display: none;
  }
  a { color: var(--accent) !important; }
}
"""


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--vale-json", type=Path, required=True)
    parser.add_argument("--readability-json", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--repo-root", type=Path, default=Path("."))
    parser.add_argument("--target-label", default="docs/")
    args = parser.parse_args()

    vale_data = load_json(args.vale_json)
    read_data = load_json(args.readability_json)
    if not isinstance(vale_data, dict):
        print("Vale JSON must be an object keyed by file path", file=sys.stderr)
        return 1
    if not isinstance(read_data, list):
        print("Readability JSON must be a list of file results", file=sys.stderr)
        return 1

    repo_root = args.repo_root.resolve()
    meta = read_vale_metadata(repo_root)
    report = build_report(vale_data, read_data, repo_root, args.target_label, meta)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(report, encoding="utf-8")
    print(f"Wrote {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
