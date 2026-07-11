#!/usr/bin/env python3
"""Generate How it works SVG (light + dark) for README.

Material Symbols icons are inlined as SVG paths (24x24 viewBox) so the
SVG is fully self-contained and GitHub-sanitize-safe (no <script>,
<style>, or external href).

Layout (LR):
    [.readignore] -> [readignore CLI] -> ┌─ [Claude Code]   ┐
                                        ├─ [codex CLI]      │ hard group
                                        └─ [pi]             ┘
                                      -> [opencode]   (solid)
                                      -.> [Cursor]    (dashed, roadmap)

Re-run: ``python scripts/gen-howitworks-svg.py`` regenerates both files.
"""

from __future__ import annotations

import os

# ---------------------------------------------------------------------------
# Material Symbols (Google, Apache-2.0) — inlined SVG paths, 24x24 viewBox.
# https://fonts.google.com/icons
# ---------------------------------------------------------------------------
ICONS: dict[str, str] = {
    # lock — used on hard (blocks-before-execution) nodes
    "lock": (
        "M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10"
        "c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2"
        "-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1"
        " 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2z"
    ),
    # shield — used on readignore CLI (the defender)
    "shield": "M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4z",
    # block — used on opencode (config deny)
    "block": (
        "M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2z"
        "m-8 10c0-4.42 3.58-8 8-8 1.85 0 3.55.63 4.9 1.69L5.69 16.9C4.63"
        " 15.55 4 13.85 4 12zm8 8c-1.85 0-3.55-.63-4.9-1.69L18.31 7.1"
        "C19.37 8.45 20 10.15 20 12c0 4.42-3.58 8-8 8z"
    ),
    # schedule — used on Cursor (roadmap)
    "schedule": (
        "M11.99 2C6.47 2 2 6.48 2 12s4.47 10 9.99 10C17.52 22 22 17.52 22"
        " 12S17.52 2 11.99 2zM12 20c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58"
        " 8 8-3.58 8-8 8zm.5-13H11v6l5.25 3.15.75-1.23-4.5-2.67z"
    ),
}

# ---------------------------------------------------------------------------
# Color schemes. Each node color is (fill, stroke). text is the body color.
# ---------------------------------------------------------------------------
LIGHT: dict = {
    "text": "#1a1a2e",
    "muted": "#5f6368",
    "arrow": "#5f6368",
    "readignore": ("#e8f0fe", "#4285f4"),  # blue
    "cli": ("#e6f4ea", "#34a853"),  # green
    "hard": ("#fef7e0", "#f9ab00"),  # orange
    "hard_group_fill": "#fffaf0",
    "hard_group_stroke": "#f9ab00",
    "opencode": ("#fce8e6", "#ea4335"),  # red
    "cursor": ("#f1f3f4", "#9aa0a6"),  # gray
}

DARK: dict = {
    "text": "#e8eaed",
    "muted": "#9aa0a6",
    "arrow": "#9aa0a6",
    "readignore": ("#1e3a5f", "#4285f4"),  # blue
    "cli": ("#1b3a2a", "#34a853"),  # green
    "hard": ("#3d2e0a", "#f9ab00"),  # orange
    "hard_group_fill": "#2a2308",
    "hard_group_stroke": "#f9ab00",
    "opencode": ("#3d1a1a", "#ea4335"),  # red
    "cursor": ("#2a2a2a", "#9aa0a6"),  # gray
}

# ---------------------------------------------------------------------------
# Geometry
# ---------------------------------------------------------------------------
SVG_W = 900
SVG_H = 420

NODE_W = 170
NODE_H = 44
NODE_RX = 8

# Column X (left edge of node)
COL_RI_X = 30
COL_CLI_X = 250
COL_AGENT_X = 560

# Y centers
CLI_Y = 180  # readignore CLI vertically centered on the hard group
RI_Y = CLI_Y

# hard group (3 nodes stacked), centered around y=120
HARD_TOP_Y = 60
HARD_NODE_GAP = 58  # center-to-center
CC_Y = HARD_TOP_Y
CX_Y = HARD_TOP_Y + HARD_NODE_GAP
PI_Y = HARD_TOP_Y + 2 * HARD_NODE_GAP

# opencode + cursor below the hard group
OC_Y = 270
CU_Y = 340

# hard group background box (encloses CC/CX/PI with padding)
HG_PAD_X = 18
HG_PAD_TOP = 28  # extra room for title
HG_PAD_BOTTOM = 14
HG_X = COL_AGENT_X - HG_PAD_X
HG_Y = HARD_TOP_Y - NODE_H // 2 - HG_PAD_TOP
HG_W = NODE_W + 2 * HG_PAD_X
HG_H = (2 * HARD_NODE_GAP) + NODE_H + HG_PAD_TOP + HG_PAD_BOTTOM


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
def _escape(text: str) -> str:
    """Escape XML special chars."""
    return (
        text.replace("&", "&amp;")
        .replace("<", "&lt;")
        .replace(">", "&gt;")
        .replace('"', "&quot;")
    )


def icon_path(name: str, x: float, y: float, color: str, size: int = 20) -> str:
    """Render a Material Symbols icon as a filled <path> at (x,y) top-left."""
    d = ICONS[name]
    # icon occupies size×size box; we translate then scale 24→size
    return (
        f'<g transform="translate({x:.1f},{y:.1f}) scale({size / 24:.4f})">'
        f'<path d="{d}" fill="{color}"/></g>'
    )


def node(
    x: float,
    y: float,
    label: str,
    sub: str | None,
    icon: str,
    fill: str,
    stroke: str,
    text_color: str,
    muted_color: str,
) -> str:
    """Rounded-rect node: icon (left) + label (right), optional sub-line.

    (x, y) is the top-left corner. Node size is NODE_W × NODE_H.
    """
    parts: list[str] = []
    # rect
    parts.append(
        f'<rect x="{x:.1f}" y="{y:.1f}" width="{NODE_W}" height="{NODE_H}" '
        f'rx="{NODE_RX}" ry="{NODE_RX}" fill="{fill}" stroke="{stroke}" '
        f'stroke-width="1.5"/>'
    )
    # icon — vertically centered, 6px from left
    icon_size = 20
    icon_x = x + 8
    icon_y = y + (NODE_H - icon_size) / 2
    parts.append(icon_path(icon, icon_x, icon_y, stroke, icon_size))
    # label — to the right of icon
    text_x = x + 8 + icon_size + 8
    text_y = y + NODE_H / 2
    if sub:
        # two lines: label bold on top, sub muted below
        parts.append(
            f'<text x="{text_x:.1f}" y="{text_y - 2:.1f}" '
            f'font-family="Segoe UI,Helvetica,Arial,sans-serif" font-size="13" '
            f'font-weight="600" fill="{text_color}" '
            f'dominant-baseline="middle">{_escape(label)}</text>'
        )
        parts.append(
            f'<text x="{text_x:.1f}" y="{text_y + 11:.1f}" '
            f'font-family="Segoe UI,Helvetica,Arial,sans-serif" font-size="10" '
            f'fill="{muted_color}" dominant-baseline="middle">'
            f'{_escape(sub)}</text>'
        )
    else:
        parts.append(
            f'<text x="{text_x:.1f}" y="{text_y:.1f}" '
            f'font-family="Segoe UI,Helvetica,Arial,sans-serif" font-size="13" '
            f'font-weight="600" fill="{text_color}" '
            f'dominant-baseline="middle">{_escape(label)}</text>'
        )
    return "".join(parts)


def _arrow_def(idx: int, color: str) -> str:
    """A <marker> arrowhead definition."""
    return (
        f'<marker id="arr{idx}" viewBox="0 0 10 10" refX="9" refY="5" '
        f'markerWidth="7" markerHeight="7" orient="auto-start-reverse">'
        f'<path d="M0,0 L10,5 L0,10 z" fill="{color}"/></marker>'
    )


def arrow(
    x1: float,
    y1: float,
    x2: float,
    y2: float,
    color: str,
    dashed: bool = False,
    marker_idx: int = 0,
) -> str:
    """A line with an arrowhead. Endpoints should land on node edges."""
    dash = ' stroke-dasharray="6,4"' if dashed else ""
    return (
        f'<line x1="{x1:.1f}" y1="{y1:.1f}" x2="{x2:.1f}" y2="{y2:.1f}" '
        f'stroke="{color}" stroke-width="2"{dash} marker-end="url(#arr'
        f'{marker_idx})"/>'
    )


def _hard_group(colors: dict) -> str:
    """Dashed background box + title around the 3 hard nodes."""
    fill = colors["hard_group_fill"]
    stroke = colors["hard_group_stroke"]
    text = colors["text"]
    muted = colors["muted"]
    parts: list[str] = []
    parts.append(
        f'<rect x="{HG_X:.1f}" y="{HG_Y:.1f}" width="{HG_W}" '
        f'height="{HG_H}" rx="10" ry="10" fill="{fill}" '
        f'fill-opacity="0.55" stroke="{stroke}" stroke-width="1.2" '
        f'stroke-dasharray="4,3"/>'
    )
    # title — top-left inside the box
    title_x = HG_X + 12
    title_y = HG_Y + 17
    parts.append(
        f'<text x="{title_x:.1f}" y="{title_y:.1f}" '
        f'font-family="Segoe UI,Helvetica,Arial,sans-serif" font-size="11" '
        f'font-weight="700" fill="{stroke}" dominant-baseline="middle">'
        f'hard &#8212; blocks before execution</text>'
    )
    return "".join(parts)


def generate(colors: dict) -> str:
    """Build the full SVG string for a theme."""
    text = colors["text"]
    muted = colors["muted"]
    arrow_c = colors["arrow"]

    ri_fill, ri_stroke = colors["readignore"]
    cli_fill, cli_stroke = colors["cli"]
    hard_fill, hard_stroke = colors["hard"]
    oc_fill, oc_stroke = colors["opencode"]
    cu_fill, cu_stroke = colors["cursor"]

    parts: list[str] = []
    # SVG root — transparent background, no width/height attr issues (use w/h)
    parts.append(
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{SVG_W}" '
        f'height="{SVG_H}" viewBox="0 0 {SVG_W} {SVG_H}" '
        f'role="img" aria-labelledby="title desc">'
        f'<title id="title">How readignore works</title>'
        f'<desc id="desc">.readignore is parsed by the readignore CLI and '
        f'adapted into per-agent defenses: hard blocks for Claude Code, '
        f'codex CLI, and pi; a config deny for opencode; a roadmap advisory '
        f'for Cursor.</desc>'
    )

    # arrow markers (one per color used; idx 0 default, 1 dashed)
    parts.append("<defs>")
    parts.append(_arrow_def(0, arrow_c))
    parts.append(_arrow_def(1, arrow_c))  # dashed uses same color
    parts.append("</defs>")

    # --- arrows (drawn first so nodes sit on top) ------------------------
    # .readignore -> readignore CLI  (horizontal)
    parts.append(
        arrow(
            COL_RI_X + NODE_W,
            RI_Y,
            COL_CLI_X,
            CLI_Y,
            arrow_c,
            dashed=False,
            marker_idx=0,
        )
    )

    # readignore CLI -> hard group (3 arrows fanning to CC/CX/PI)
    cli_right_x = COL_CLI_X + NODE_W
    for ty in (CC_Y, CX_Y, PI_Y):
        parts.append(
            arrow(
                cli_right_x,
                CLI_Y,
                COL_AGENT_X,
                ty,
                arrow_c,
                dashed=False,
                marker_idx=0,
            )
        )

    # readignore CLI -> opencode (solid)
    parts.append(
        arrow(
            cli_right_x,
            CLI_Y,
            COL_AGENT_X,
            OC_Y,
            arrow_c,
            dashed=False,
            marker_idx=0,
        )
    )

    # readignore CLI -> Cursor (dashed, roadmap)
    parts.append(
        arrow(
            cli_right_x,
            CLI_Y,
            COL_AGENT_X,
            CU_Y,
            arrow_c,
            dashed=True,
            marker_idx=1,
        )
    )

    # --- hard group background -------------------------------------------
    parts.append(_hard_group(colors))

    # --- nodes ------------------------------------------------------------
    # .readignore
    parts.append(
        node(
            COL_RI_X,
            RI_Y - NODE_H / 2,
            ".readignore",
            "gitignore syntax",
            "shield",
            ri_fill,
            ri_stroke,
            text,
            muted,
        )
    )
    # readignore CLI
    parts.append(
        node(
            COL_CLI_X,
            CLI_Y - NODE_H / 2,
            "readignore CLI",
            "parse + adapt",
            "shield",
            cli_fill,
            cli_stroke,
            text,
            muted,
        )
    )
    # hard nodes
    parts.append(
        node(
            COL_AGENT_X,
            CC_Y - NODE_H / 2,
            "Claude Code",
            "PreToolUse hook",
            "lock",
            hard_fill,
            hard_stroke,
            text,
            muted,
        )
    )
    parts.append(
        node(
            COL_AGENT_X,
            CX_Y - NODE_H / 2,
            "codex CLI",
            "PreToolUse hook",
            "lock",
            hard_fill,
            hard_stroke,
            text,
            muted,
        )
    )
    parts.append(
        node(
            COL_AGENT_X,
            PI_Y - NODE_H / 2,
            "pi",
            "read tool override",
            "lock",
            hard_fill,
            hard_stroke,
            text,
            muted,
        )
    )
    # opencode
    parts.append(
        node(
            COL_AGENT_X,
            OC_Y - NODE_H / 2,
            "opencode",
            "permission.read deny",
            "block",
            oc_fill,
            oc_stroke,
            text,
            muted,
        )
    )
    # Cursor
    parts.append(
        node(
            COL_AGENT_X,
            CU_Y - NODE_H / 2,
            "Cursor",
            "roadmap",
            "schedule",
            cu_fill,
            cu_stroke,
            text,
            muted,
        )
    )

    parts.append("</svg>")
    return "".join(parts)


def main() -> None:
    os.makedirs("docs/images", exist_ok=True)
    light = generate(LIGHT)
    dark = generate(DARK)
    with open("docs/images/how-it-works-light.svg", "w", encoding="utf-8") as f:
        f.write(light)
    with open("docs/images/how-it-works-dark.svg", "w", encoding="utf-8") as f:
        f.write(dark)
    print("Generated: docs/images/how-it-works-{light,dark}.svg")


if __name__ == "__main__":
    main()
