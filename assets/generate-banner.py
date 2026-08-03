#!/usr/bin/env python3
"""Regenerate the README banner (light + dark) into assets/.

Run from the repository root:  python3 assets/generate-banner.py
"""

import os

HERE = os.path.dirname(os.path.abspath(__file__))

# Inline the official Firefly III icon, minus its <svg> wrapper, so the banner
# stays a single self-contained file.
_icon = open(os.path.join(HERE, "firefly-iii-icon.svg")).read()
FF_INNER = _icon[_icon.index(">", _icon.index("<svg")) + 1:_icon.rindex("</svg>")].strip()

FONT = "ui-sans-serif,-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif"
MONO = "ui-monospace,SFMono-Regular,'SF Mono',Menlo,Consolas,'Liberation Mono',monospace"

THEMES = {
    "light": dict(
        card="#ffffff", cardEdge="#d8dee4", inner="#f6f8fa", innerEdge="#d8dee4",
        text="#1f2328", muted="#59636e", faint="#818b98", wire="#9aa4af",
        tint=0.13, pill="#ffffff",
    ),
    "dark": dict(
        card="#0d1117", cardEdge="#30363d", inner="#161b22", innerEdge="#30363d",
        text="#e6edf3", muted="#9198a1", faint="#7d8590", wire="#6e7681",
        tint=0.22, pill="#161b22",
    ),
}

# stage accents (identical in both themes)
BLUE, GREEN, PURPLE, AMBER, ORANGE = "#3b82f6", "#22a55b", "#8b5cf6", "#e0a30c", "#cd5029"
CHIP = ["#2563eb", "#0d9488", "#7c3aed", "#db2777"]

W, H = 1080, 390
LANE1 = 158           # main pipeline centre line
LANE2 = 300           # portfolio-sync lane centre line
NODE_W, NODE_H, GAP = 158, 104, 18
NODE_X0 = 190
NODE_Y = LANE1 - NODE_H // 2          # 106


def nx(i):
    return NODE_X0 + i * (NODE_W + GAP)


TILE_X, TILE_Y, TILE_S = 952, 181, 96
TILE_CX = TILE_X + TILE_S // 2


def esc(s):
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def txt(x, y, s, size=12, fill="#000", weight="400", anchor="start",
        ls=None, family=FONT, opacity=None):
    a = [f'x="{x}"', f'y="{y}"', f'font-family="{family}"', f'font-size="{size}"',
         f'font-weight="{weight}"', f'fill="{fill}"', f'text-anchor="{anchor}"']
    if ls is not None:
        a.append(f'letter-spacing="{ls}"')
    if opacity is not None:
        a.append(f'opacity="{opacity}"')
    return f'<text {" ".join(a)}>{esc(s)}</text>'


def arrow_right(x, y, c, w=8, h=4.6):
    return f'<path d="M{x - w} {y - h} L{x} {y} L{x - w} {y + h} Z" fill="{c}"/>'


def arrow_down(x, y, c, w=4.6, h=8):
    return f'<path d="M{x - w} {y - h} L{x} {y} L{x + w} {y - h} Z" fill="{c}"/>'


# ---------------------------------------------------------------- glyphs
def g_bank(c):
    return (f'<path d="M3 9.4 L12 3.6 L21 9.4 Z" fill="{c}"/>'
            f'<rect x="5.2" y="11.2" width="2.9" height="7.2" rx="0.6" fill="{c}"/>'
            f'<rect x="10.55" y="11.2" width="2.9" height="7.2" rx="0.6" fill="{c}"/>'
            f'<rect x="15.9" y="11.2" width="2.9" height="7.2" rx="0.6" fill="{c}"/>'
            f'<rect x="3" y="19.4" width="18" height="2.4" rx="1.2" fill="{c}"/>')


def g_card(c):
    return (f'<rect x="2.2" y="5" width="19.6" height="14" rx="2.6" fill="none" stroke="{c}" stroke-width="1.9"/>'
            f'<rect x="2.2" y="8.2" width="19.6" height="3.2" fill="{c}"/>'
            f'<rect x="5.4" y="14" width="6.4" height="1.9" rx="0.95" fill="{c}"/>')


def g_chart(c):
    return (f'<rect x="3.2" y="13.4" width="4" height="7.2" rx="1.3" fill="{c}" opacity="0.55"/>'
            f'<rect x="10" y="8.8" width="4" height="11.8" rx="1.3" fill="{c}" opacity="0.78"/>'
            f'<rect x="16.8" y="3.4" width="4" height="17.2" rx="1.3" fill="{c}"/>')


def g_coins(c):
    return (f'<ellipse cx="12" cy="17.6" rx="8" ry="3.2" fill="{c}" opacity="0.45"/>'
            f'<ellipse cx="12" cy="12.6" rx="8" ry="3.2" fill="{c}" opacity="0.72"/>'
            f'<ellipse cx="12" cy="7.6" rx="8" ry="3.2" fill="{c}"/>')


CHIP_GLYPHS = [g_bank, g_card, g_chart, g_coins]


def g_browser(c):
    return (f'<rect x="2.4" y="4" width="19.2" height="16" rx="2.6" fill="none" stroke="{c}" stroke-width="1.9"/>'
            f'<path d="M2.4 9 H21.6" stroke="{c}" stroke-width="1.9"/>'
            f'<circle cx="5.9" cy="6.5" r="1.05" fill="{c}"/>'
            f'<circle cx="9.3" cy="6.5" r="1.05" fill="{c}"/>'
            f'<path d="M10.6 12.4 L17.4 15.1 L14.5 16.1 L13.4 19 Z" fill="{c}"/>')


def g_sheet(c):
    return (f'<path d="M5 2.6 H14.2 L19.6 8 V21.4 H5 Z" fill="none" stroke="{c}" '
            f'stroke-width="1.9" stroke-linejoin="round"/>'
            f'<path d="M13.9 2.8 V8.2 H19.4" fill="none" stroke="{c}" stroke-width="1.7" stroke-linejoin="round"/>'
            f'<path d="M7.8 12.6 H16.8 M7.8 15.6 H16.8 M7.8 18.4 H16.8 M11.4 11.4 V19.6" '
            f'stroke="{c}" stroke-width="1.35" stroke-linecap="round" opacity="0.85"/>')


def g_shield(c):
    return (f'<path d="M12 2.4 L20 5.6 V11.4 C20 16.6 16.6 20.4 12 22 '
            f'C7.4 20.4 4 16.6 4 11.4 V5.6 Z" fill="none" stroke="{c}" '
            f'stroke-width="1.9" stroke-linejoin="round"/>'
            f'<path d="M8.6 11.9 L11.1 14.5 L15.6 9.8" fill="none" stroke="{c}" '
            f'stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"/>')


def g_spark(c):
    return (f'<path d="M13.6 2.2 L15.6 8 L21.4 10 L15.6 12 L13.6 17.8 L11.6 12 L5.8 10 L11.6 8 Z" fill="{c}"/>'
            f'<path d="M6.4 15 L7.4 17.8 L10.2 18.8 L7.4 19.8 L6.4 22.6 L5.4 19.8 L2.6 18.8 L5.4 17.8 Z" '
            f'fill="{c}" opacity="0.7"/>')


def g_key(c):
    return (f'<circle cx="7.4" cy="12" r="4.3" fill="none" stroke="{c}" stroke-width="1.9"/>'
            f'<path d="M11.4 12 H21 M18.2 12 V15.6 M14.9 12 V15.1" stroke="{c}" '
            f'stroke-width="1.9" stroke-linecap="round"/>')


def g_cpu(c):
    return (f'<rect x="6.4" y="6.4" width="11.2" height="11.2" rx="2.4" fill="none" stroke="{c}" stroke-width="1.9"/>'
            f'<rect x="10" y="10" width="4" height="4" rx="1" fill="{c}"/>'
            f'<path d="M9.4 2.8 V6.4 M14.6 2.8 V6.4 M9.4 17.6 V21.2 M14.6 17.6 V21.2 '
            f'M2.8 9.4 H6.4 M2.8 14.6 H6.4 M17.6 9.4 H21.2 M17.6 14.6 H21.2" '
            f'stroke="{c}" stroke-width="1.7" stroke-linecap="round"/>')


def g_trend(c):
    return (f'<path d="M3 17.8 L9.2 11.4 L13 15 L20.4 7" fill="none" stroke="{c}" '
            f'stroke-width="2.1" stroke-linecap="round" stroke-linejoin="round"/>'
            f'<path d="M14.8 6.6 H21 V12.8" fill="none" stroke="{c}" stroke-width="2.1" '
            f'stroke-linecap="round" stroke-linejoin="round"/>')


def g_sync(c):
    return (f'<path d="M20 12 A8 8 0 0 1 6.6 17.9" fill="none" stroke="{c}" stroke-width="2" stroke-linecap="round"/>'
            f'<path d="M4 12 A8 8 0 0 1 17.4 6.1" fill="none" stroke="{c}" stroke-width="2" stroke-linecap="round"/>'
            f'<path d="M17.4 2.6 L18 6.6 L14 6.9 Z" fill="{c}"/>'
            f'<path d="M6.6 21.4 L6 17.4 L10 17.1 Z" fill="{c}"/>')


def glyph(fn, color, cx, cy, size):
    s = size / 24.0
    return (f'<g transform="translate({cx - size / 2:.2f},{cy - size / 2:.2f}) scale({s:.4f})">'
            f'{fn(color)}</g>')


# ---------------------------------------------------------------- build
def build(theme_name):
    t = THEMES[theme_name]
    o = []
    o.append(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {W} {H}" width="{W}" height="{H}" '
             f'role="img" aria-label="Firefly Bridge pipeline: banks, cards and brokerages are scraped '
             f'with a headless browser, parsed from CSV or XLSX, de-duplicated with SHA-256 hashes, '
             f'categorised by an LLM, and imported into Firefly III; portfolio-sync feeds market prices in alongside.">')

    # card
    o.append(f'<rect x="0.75" y="0.75" width="{W - 1.5}" height="{H - 1.5}" rx="16" '
             f'fill="{t["card"]}" stroke="{t["cardEdge"]}" stroke-width="1.5"/>')

    # ---- header
    o.append(txt(28, 32, "FIREFLY BRIDGE", 13.5, t["text"], "700", ls="2.6"))
    o.append(txt(W - 28, 32, "semi-automatic, deterministic, config-driven", 12,
                 t["faint"], "400", anchor="end"))

    # ---- feeder pills (dotted, above lane 1)
    feeders = [
        (nx(0) + NODE_W / 2, 300, "1Password · Bitwarden", "op:// · bw://", g_key, BLUE),
        (nx(3) + NODE_W / 2, 230, "OpenAI-compatible", "LLM", g_cpu, AMBER),
    ]
    for cx, pw, label, code, gl, col in feeders:
        px, py, ph = cx - pw / 2, 50, 32
        o.append(f'<rect x="{px:.1f}" y="{py}" width="{pw}" height="{ph}" rx="{ph / 2}" '
                 f'fill="{t["inner"]}" stroke="{t["innerEdge"]}" stroke-width="1.2"/>')
        o.append(glyph(gl, col, px + 20, py + ph / 2, 15))
        o.append(txt(px + 33, py + ph / 2 + 4.3, label, 12.5, t["muted"], "500"))
        # Right-anchored: the exact label width depends on whichever font the
        # viewer resolves, so pin the code refs to the pill's right padding
        # instead of guessing where the label ends.
        o.append(txt(px + pw - 16, py + ph / 2 + 4.3, code, 11.5, t["faint"], "500",
                     anchor="end", family=MONO))
        # dotted drop line into the node
        o.append(f'<path d="M{cx} {py + ph + 2} V{NODE_Y - 12}" stroke="{t["wire"]}" '
                 f'stroke-width="1.6" stroke-dasharray="2 4" stroke-linecap="round" opacity="0.8"/>')
        o.append(arrow_down(cx, NODE_Y - 3, t["wire"]))

    # ---- institutions cluster (left end)
    cl_cx, chip = 98, 52
    cx0, cy0 = cl_cx - chip - 5, LANE1 - chip - 5
    for i in range(4):
        gx = cx0 + (i % 2) * (chip + 10)
        gy = cy0 + (i // 2) * (chip + 10)
        o.append(f'<rect x="{gx}" y="{gy}" width="{chip}" height="{chip}" rx="15" fill="{CHIP[i]}"/>')
        o.append(f'<rect x="{gx}" y="{gy}" width="{chip}" height="{chip}" rx="15" fill="#ffffff" opacity="0.10"/>')
        o.append(glyph(CHIP_GLYPHS[i], "#ffffff", gx + chip / 2, gy + chip / 2, 28))
    o.append(txt(cl_cx, LANE1 + chip + 27, "INSTITUTIONS", 10.5, t["muted"], "700",
                 anchor="middle", ls="1.6"))
    o.append(txt(cl_cx, LANE1 + chip + 44, "banks · cards · brokerages", 10.5, t["faint"],
                 "400", anchor="middle"))

    # arrow: institutions -> stage 1
    o.append(f'<path d="M{cl_cx + chip + 12} {LANE1} H{nx(0) - 14}" stroke="{t["wire"]}" '
             f'stroke-width="2" stroke-linecap="round"/>')
    o.append(arrow_right(nx(0) - 5, LANE1, t["wire"]))

    # ---- main pipeline stages
    stages = [
        (g_browser, BLUE, "Browser", "login · download"),
        (g_sheet, GREEN, "Parse", "CSV · XLSX"),
        (g_shield, PURPLE, "Dedupe", "SHA-256 hash"),
        (g_spark, AMBER, "Categorize", "category · budget"),
    ]
    for i, (gl, col, title, sub) in enumerate(stages):
        x = nx(i)
        o.append(f'<rect x="{x}" y="{NODE_Y}" width="{NODE_W}" height="{NODE_H}" rx="14" '
                 f'fill="{t["inner"]}" stroke="{t["innerEdge"]}" stroke-width="1.4"/>')
        bx, by, bs = x + NODE_W / 2 - 17, NODE_Y + 12, 34
        o.append(f'<rect x="{bx}" y="{by}" width="{bs}" height="{bs}" rx="10" '
                 f'fill="{col}" opacity="{t["tint"]}"/>')
        o.append(glyph(gl, col, bx + bs / 2, by + bs / 2, 21))
        o.append(txt(x + NODE_W / 2, NODE_Y + 70, title, 15.5, t["text"], "600", anchor="middle"))
        o.append(txt(x + NODE_W / 2, NODE_Y + 88, sub, 12, t["muted"], "400", anchor="middle"))
        if i < 3:
            o.append(f'<path d="M{x + NODE_W + 3} {LANE1} H{x + NODE_W + GAP - 9}" '
                     f'stroke="{t["wire"]}" stroke-width="2" stroke-linecap="round"/>')
            o.append(arrow_right(x + NODE_W + GAP - 1, LANE1, t["wire"]))

    # ---- portfolio-sync lane
    lane2 = [
        (190, 210, g_trend, GREEN, "Market prices", "yahoo · moneycontrol · kitco"),
        (452, 236, g_sync, PURPLE, "portfolio-sync", "holdings → profit / loss"),
    ]
    for px, pw, gl, col, title, sub in lane2:
        py, ph = LANE2 - 26, 52
        o.append(f'<rect x="{px}" y="{py}" width="{pw}" height="{ph}" rx="13" '
                 f'fill="{t["inner"]}" stroke="{t["innerEdge"]}" stroke-width="1.4"/>')
        o.append(f'<rect x="{px + 13}" y="{py + 11}" width="30" height="30" rx="9" '
                 f'fill="{col}" opacity="{t["tint"]}"/>')
        o.append(glyph(gl, col, px + 28, py + 26, 19))
        o.append(txt(px + 53, LANE2 - 3, title, 13.5, t["text"], "600"))
        o.append(txt(px + 53, LANE2 + 14, sub, 11, t["muted"], "400"))
    o.append(f'<path d="M{190 + 210 + 3} {LANE2} H{452 - 15}" stroke="{t["wire"]}" '
             f'stroke-width="2" stroke-linecap="round"/>')
    o.append(arrow_right(452 - 5, LANE2, t["wire"]))

    # ---- converging elbows into Firefly III
    o.append(f'<path d="M{nx(3) + NODE_W + 3} {LANE1} H876 Q900 {LANE1} 900 182 V181 '
             f'Q900 205 924 205 H{TILE_X - 14}" fill="none" stroke="{t["wire"]}" '
             f'stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>')
    o.append(arrow_right(TILE_X - 5, 205, t["wire"]))
    o.append(f'<path d="M{452 + 236 + 3} {LANE2} H876 Q900 {LANE2} 900 277 V277 '
             f'Q900 253 924 253 H{TILE_X - 14}" fill="none" stroke="{t["wire"]}" '
             f'stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>')
    o.append(arrow_right(TILE_X - 5, 253, t["wire"]))

    # ---- Firefly III tile
    o.append(f'<rect x="{TILE_X - 7}" y="{TILE_Y - 7}" width="{TILE_S + 14}" height="{TILE_S + 14}" '
             f'rx="30" fill="{ORANGE}" opacity="0.14"/>')
    o.append(f'<clipPath id="ffclip-{theme_name}"><rect x="{TILE_X}" y="{TILE_Y}" '
             f'width="{TILE_S}" height="{TILE_S}" rx="24"/></clipPath>')
    sc = TILE_S / 377.95276
    o.append(f'<g clip-path="url(#ffclip-{theme_name})">'
             f'<g transform="translate({TILE_X},{TILE_Y}) scale({sc:.6f})">{FF_INNER}</g></g>')
    o.append(txt(TILE_CX, TILE_Y + TILE_S + 24, "Firefly III", 14, t["text"], "600", anchor="middle"))

    # ---- footnote
    o.append(f'<path d="M28 {H - 46} H{W - 28}" stroke="{t["innerEdge"]}" stroke-width="1"/>')
    o.append(txt(28, H - 25, "one-time bootstrap · backfill-hashes stamps internal_reference onto "
                             "pre-existing transactions so nothing is imported twice",
                 11.5, t["faint"], "400"))
    o.append(txt(W - 28, H - 25, "every run is tagged  bridge-<timestamp>", 11.5, t["faint"],
                 "400", anchor="end", family=MONO))

    o.append('</svg>')
    return "\n".join(o) + "\n"


for name in THEMES:
    path = os.path.join(HERE, f"banner-{name}.svg")
    open(path, "w").write(build(name))
    print("wrote", path)
