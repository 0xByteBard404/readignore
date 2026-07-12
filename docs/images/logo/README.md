# readignore — brand assets

Final logo set. Direction: **"no-read eye"** — eye outline + diamond pupil + prohibition slash.
"read + ignore" = a vision/agent eye, blocked.

## Palette

| Role | Light mode | Dark mode |
|---|---|---|
| Mark / eye | `#0D9488` (teal-600) | `#2DD4BF` (teal-400) |
| Slash (prohibit) | `#F4511E` (orange-red) | `#FF7849` (orange-400) |
| Ink / wordmark | `#0F172A` | `#F1F5F9` |
| Card background | gradient `#0E1B18 → #070D0C` |

**Why teal + orange-red** (research-backed, not taste):
- Escapes the **"AI purple/blue" cliché** (the documented "Purple Problem").
- Teal = 2026 standout color + reads as **protect / security** — fits "protect files from being read".
- Orange-red = teal's **color-wheel complement** (max 2-color contrast) and carries **"prohibited"** semantics.
- Both pass **WCAG AA-large** on light and dark backgrounds.

## Files

| File | What |
|---|---|
| `readignore-mark.svg` | Canonical icon, transparent (the brand mark) |
| `readignore-hero-light.svg` / `readignore-hero-dark.svg` | Mark + wordmark lockup (used in README) |
| `social-preview.png` (+ `.svg` source) | GitHub social card, 1280×640 |
| `favicon.svg` | Vector favicon (regenerate raster sizes when there is a site) |

## Usage

Paths below are relative to repo root.

**README hero (auto light/dark):**
```html
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/logo/readignore-hero-dark.svg">
  <img src="docs/images/logo/readignore-hero-light.svg" alt="readignore" height="64">
</picture>
```

**GitHub social preview:** Repo → Settings → General → “Social preview” → upload `docs/images/logo/social-preview.png`.

**Favicon (docs site / GitHub Pages, when one exists):**
```html
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
```
Raster favicon sizes (`.ico`, 16/32/48 PNGs, apple-touch-icon) are omitted until there is a site to serve them — regenerate from `favicon.svg` with `scripts/svg2png.py` + Pillow.
