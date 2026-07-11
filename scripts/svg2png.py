#!/usr/bin/env python3
"""Convert SVG files to PNG (and optionally JPG).

Used for previews where SVG is not supported — social-media share cards,
some markdown renderers, certain doc viewers, etc.

Backends (tried in order):

1. ``cairosvg`` — pure-Python SVG renderer on top of cairo.
   Cleanest API (``cairosvg.svg2png``). May need the cairo DLLs on PATH
   on Windows (``pip install cairosvg`` plus a cairo runtime).
   PNG output preserves the SVG's transparent background.
2. ``svglib`` + ``reportlab`` — parses SVG into a reportlab Drawing,
   then renders to PNG/JPG via ``renderPM``. No native cairo dependency,
   usually the most reliable on stock Windows.
   Note: svglib composites onto an opaque background, so PNG output is
   RGB on white rather than transparent. If you need transparency, use
   the cairosvg backend (or post-process with Pillow).

If neither is importable, the script prints an install hint and exits 1.

Usage::

    # single file -> .png (same dir, same name)
    python scripts/svg2png.py docs/images/how-it-works-light.svg

    # all docs/images/*.svg
    python scripts/svg2png.py --all

    # format + width
    python scripts/svg2png.py docs/images/how-it-works-light.svg --format png --width 1800
    python scripts/svg2png.py docs/images/how-it-works-light.svg --format jpg --width 1800

Defaults: width 1800px. JPG is always composited on white; PNG keeps
transparency under the cairosvg backend and is RGB-on-white under svglib.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

# ---------------------------------------------------------------------------
# Backend detection
# ---------------------------------------------------------------------------
# cairosvg imports cleanly but dlopen()'s the cairo shared library lazily;
# on stock Windows the cairo DLL is usually missing, raising OSError at
# import time. We treat both ImportError and OSError as "cairosvg unusable"
# and fall through to the svglib backend.
try:
    import cairosvg  # type: ignore[import-not-found]

    BACKEND = "cairosvg"
except (ImportError, OSError):  # pragma: no cover - environment dependent
    try:
        from svglib import svglib  # type: ignore[import-not-found]
        from reportlab.graphics import renderPM  # type: ignore[import-not-found]  # noqa: F401

        BACKEND = "svglib"
    except ImportError:
        sys.stderr.write(
            "No SVG backend found. Install one of:\n"
            "    pip install cairosvg            # (may also need cairo DLLs)\n"
            "    pip install svglib reportlab    # (pure Python, no native deps)\n"
        )
        sys.exit(1)


DEFAULT_WIDTH = 1800
REPO_ROOT = Path(__file__).resolve().parent.parent
DEFAULT_SVG_GLOB = "docs/images/*.svg"


# ---------------------------------------------------------------------------
# Conversion
# ---------------------------------------------------------------------------
def _convert_cairosvg(
    src: Path,
    dst: Path,
    fmt: str,
    width: int,
) -> None:
    """Render via cairosvg. JPG is PNG-then-PIL-composite on white."""
    if fmt == "png":
        cairosvg.svg2png(
            url=str(src),
            write_to=str(dst),
            output_width=width,
        )
        return

    # jpg — cairosvg has no direct jpg writer; render PNG to a temp buffer,
    # then flatten onto white via Pillow (lazy import so the png-only path
    # does not require Pillow).
    import io

    from PIL import Image  # type: ignore[import-not-found]

    buf = io.BytesIO()
    cairosvg.svg2png(
        url=str(src),
        write_to=buf,
        output_width=width,
    )
    buf.seek(0)
    img = Image.open(buf).convert("RGBA")
    bg = Image.new("RGBA", img.size, (255, 255, 255, 255))
    bg.alpha_composite(img)
    bg.convert("RGB").save(dst, format="JPEG", quality=92)


def _convert_svglib(
    src: Path,
    dst: Path,
    fmt: str,
    width: int,
) -> None:
    """Render via svglib + reportlab renderPM.

    svglib returns a reportlab Drawing sized in points matching the SVG's
    intrinsic size; we scale the Drawing so its width equals ``width`` and
    let renderPM rasterize at that scale.
    """
    drawing = svglib.svg2rlg(str(src))
    if drawing is None:
        raise RuntimeError(f"svglib could not parse {src}")

    # Scale to target width. Drawing.width is in points (== user units for
    # our SVGs, which carry no explicit width/height unit).
    if drawing.width <= 0:
        raise RuntimeError(f"svglib reported non-positive width for {src}")
    scale = width / drawing.width
    drawing.scale(scale, scale)
    drawing.width *= scale
    drawing.height *= scale

    if fmt == "png":
        renderPM.drawToFile(drawing, str(dst), fmt="PNG")
        return

    # jpg — renderPM can write JPEG directly via fmt="JPEG"
    renderPM.drawToFile(drawing, str(dst), fmt="JPEG")


def convert_one(src: Path, fmt: str, width: int) -> Path:
    """Convert a single SVG file. Returns the output path written."""
    if not src.is_file():
        raise FileNotFoundError(src)

    dst = src.with_suffix(f".{fmt}")
    if BACKEND == "cairosvg":
        _convert_cairosvg(src, dst, fmt, width)
    else:
        _convert_svglib(src, dst, fmt, width)
    return dst


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------
def _build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        description="Convert SVG to PNG/JPG (cairosvg, svglib fallback).",
    )
    p.add_argument(
        "svg",
        nargs="?",
        help="SVG file to convert. Ignored when --all is set.",
    )
    p.add_argument(
        "--all",
        action="store_true",
        help=f"Convert every {DEFAULT_SVG_GLOB} under the repo root.",
    )
    p.add_argument(
        "--format",
        choices=("png", "jpg"),
        default="png",
        help="Output format (default: png). PNG keeps transparency; "
        "JPG is composited on white.",
    )
    p.add_argument(
        "--width",
        type=int,
        default=DEFAULT_WIDTH,
        help=f"Output width in pixels (default: {DEFAULT_WIDTH}). Height "
        "scales proportionally to preserve aspect ratio.",
    )
    return p


def _resolve_targets(args: argparse.Namespace) -> list[Path]:
    if args.all:
        return sorted(REPO_ROOT.glob(DEFAULT_SVG_GLOB))
    if not args.svg:
        sys.stderr.write("error: provide an SVG file or use --all\n")
        sys.exit(2)
    p = Path(args.svg)
    if not p.is_absolute():
        p = (Path.cwd() / p).resolve()
    return [p]


def main() -> None:
    args = _build_parser().parse_args()
    targets = _resolve_targets(args)
    if not targets:
        sys.stderr.write(f"no SVG files matched\n")
        sys.exit(1)

    print(f"backend: {BACKEND}")
    for src in targets:
        try:
            dst = convert_one(src, args.format, args.width)
        except Exception as e:  # noqa: BLE001
            sys.stderr.write(f"FAIL  {src} -> {e}\n")
            sys.exit(1)
        print(f"ok    {src} -> {dst}  ({args.width}px, {args.format})")


if __name__ == "__main__":
    main()
