#!/usr/bin/env python3
"""Augment a copy of a terminal font with OpenUsage provider-icon glyphs.

The original font is never modified. A renamed copy is written to --out so it
can coexist with the original in Font Book / iTerm2.

Run with the fonttools venv, e.g.:
    /tmp/fontvenv/bin/python scripts/patch-terminal-font.py \
        --base ~/Library/Fonts/MyFont.otf \
        --out  /tmp/MyFont-OpenUsage.otf
"""

import argparse
import json
import os
import sys
import xml.etree.ElementTree as ET

from fontTools.ttLib import TTFont
from fontTools.pens.transformPen import TransformPen
from fontTools.pens.ttGlyphPen import TTGlyphPen
from fontTools.pens.t2CharStringPen import T2CharStringPen
from fontTools.pens.cu2quPen import Cu2QuPen
from fontTools.svgLib.path import parse_path


def repo_root():
    # scripts/ lives directly under the repo root.
    return os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def extract_path_ds(svg_path):
    """Return all `d` attributes from <path> elements, namespace-agnostic."""
    tree = ET.parse(svg_path)
    root = tree.getroot()
    ds = []
    for el in root.iter():
        tag = el.tag
        # Strip XML namespace if present: '{http://...}path' -> 'path'.
        if "}" in tag:
            tag = tag.split("}", 1)[1]
        if tag == "path":
            d = el.get("d")
            if d:
                ds.append(d)
    return ds


def detect_format(font):
    if "glyf" in font:
        return "glyf"
    if "CFF " in font or "CFF2" in font:
        return "CFF"
    raise SystemExit("error: base font has neither 'glyf' nor 'CFF '/'CFF2' tables")


def choose_advance(font, upem):
    """Pick a monospaced advance width: prefer 'M', then 'space', then 0.6*upem."""
    hmtx = font["hmtx"]
    cmap = font.getBestCmap()
    for cp in (ord("M"), ord("m"), ord("0")):
        name = cmap.get(cp)
        if name and name in hmtx.metrics:
            adv = hmtx.metrics[name][0]
            if adv > 0:
                return adv
    if "space" in hmtx.metrics and hmtx.metrics["space"][0] > 0:
        return hmtx.metrics["space"][0]
    return round(0.6 * upem)


def choose_cap_height(font, upem):
    if "OS/2" in font:
        ch = getattr(font["OS/2"], "sCapHeight", 0) or 0
        if ch > 0:
            return ch
    return round(0.7 * upem)


def make_transform(scale, xoff, yoff=0.0):
    # SVG is y-down with origin at top of a 24x24 box. Fonts are y-up from the
    # baseline. Map svg(x, y) -> (x*scale + xoff, (24 - y)*scale + yoff).
    #   X = x*scale + xoff
    #   Y = -y*scale + 24*scale + yoff
    # Affine (a, b, c, d, e, f): X = a*x + c*y + e ; Y = b*x + d*y + f
    return (scale, 0.0, 0.0, -scale, xoff, 24.0 * scale + yoff)


def add_unique_name(order_set, base_name):
    name = base_name
    i = 1
    while name in order_set:
        name = "%s_%d" % (base_name, i)
        i += 1
    return name


def build_glyf_glyph(ds, transform, advance):
    tt_pen = TTGlyphPen(None)
    # Cubic -> quadratic conversion, then transform into font units.
    cu2qu = Cu2QuPen(tt_pen, max_err=1.0, reverse_direction=True)
    pen = TransformPen(cu2qu, transform)
    for d in ds:
        parse_path(d, pen)
    return tt_pen.glyph()


def build_cff_charstring(ds, transform, advance):
    t2_pen = T2CharStringPen(advance, None)
    pen = TransformPen(t2_pen, transform)
    for d in ds:
        parse_path(d, pen)
    return t2_pen.getCharString()


def insert_glyf(font, name, glyph, advance):
    font["glyf"][name] = glyph
    font["hmtx"][name] = (advance, 0)


def insert_cff(font, name, charstring, advance):
    cff = font["CFF "].cff
    top_dict_name = cff.fontNames[0]
    top_dict = cff[top_dict_name]
    char_strings = top_dict.CharStrings
    # Bind the charstring to this font's private dict / global subrs so it can
    # be re-serialized (decompile depends on these).
    private = top_dict.Private
    charstring.private = private
    charstring.globalSubrs = char_strings.globalSubrs
    if char_strings.charStringsAreIndexed:
        # Loaded from an OTF: charStrings maps name -> index into the index
        # list. Append the new charstring and register the name.
        index = char_strings.charStringsIndex
        index.append(charstring)
        char_strings.charStrings[name] = len(index) - 1
    else:
        char_strings.charStrings[name] = charstring
    # Keep the charset (the ordered list the table serializes from) in sync.
    if hasattr(top_dict, "charset") and name not in top_dict.charset:
        top_dict.charset.append(name)
    font["hmtx"][name] = (advance, 0)


def register_glyph_order(font, name):
    order = font.getGlyphOrder()
    if name not in order:
        order = list(order)
        order.append(name)
        font.setGlyphOrder(order)


def add_to_cmaps(font, codepoint, name):
    cmap_table = font["cmap"]
    for sub in cmap_table.tables:
        if sub.isUnicode():
            sub.cmap[codepoint] = name


def rename_font(font, suffix):
    name_table = font["name"]

    def ps_suffix():
        # PostScript names contain no spaces.
        return "-" + suffix.strip().replace(" ", "").lstrip("+")

    for rec in name_table.names:
        nid = rec.nameID
        cur = rec.toUnicode()
        if nid in (1, 4, 16):
            if not cur.endswith(suffix):
                name_table.setName(cur + suffix, nid, rec.platformID,
                                   rec.platEncID, rec.langID)
        elif nid == 6:
            new = cur + ps_suffix()
            name_table.setName(new, nid, rec.platformID, rec.platEncID, rec.langID)
        elif nid == 3:
            new = cur + suffix.strip().replace(" ", "")
            name_table.setName(new, nid, rec.platformID, rec.platEncID, rec.langID)


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base", required=True, help="path to base font (.otf or .ttf)")
    ap.add_argument("--out", required=True, help="output font path")
    ap.add_argument("--name-suffix", default=" +OpenUsage",
                    help="suffix appended to family/full/typographic names")
    ap.add_argument("--manifest",
                    default=os.path.join(repo_root(), "internal", "tmux",
                                         "assets", "icons.json"))
    ap.add_argument("--svg-dir",
                    default=os.path.join(repo_root(), "website", "public", "icons"))
    args = ap.parse_args()

    font = TTFont(args.base)
    fmt = detect_format(font)
    upem = font["head"].unitsPerEm
    advance = choose_advance(font, upem)
    cap_height = choose_cap_height(font, upem)
    target_height = cap_height
    scale = target_height / 24.0
    # Icons are square (24x24). If scaling to cap height would make the glyph
    # wider than one cell, clamp to the advance so it stays inside the cell.
    if 24.0 * scale > advance:
        scale = advance / 24.0
    glyph_size = 24.0 * scale
    xoff = (advance - glyph_size) / 2.0
    # Vertically center the (possibly clamped) icon on the cap-height band.
    yoff = (cap_height - glyph_size) / 2.0
    transform = make_transform(scale, xoff, yoff)

    with open(args.manifest) as fh:
        manifest = json.load(fh)

    order_set = set(font.getGlyphOrder())
    added = 0

    for entry in manifest["glyphs"]:
        provider = entry["provider"]
        svg = entry["svg"]
        codepoint = int(entry["codepoint"], 16)
        svg_path = os.path.join(args.svg_dir, svg + ".svg")
        if not os.path.exists(svg_path):
            print("warn: missing svg %s, skipping %s" % (svg_path, provider),
                  file=sys.stderr)
            continue
        ds = extract_path_ds(svg_path)
        if not ds:
            print("warn: no <path d> in %s, skipping %s" % (svg_path, provider),
                  file=sys.stderr)
            continue

        name = add_unique_name(order_set, "ouicon_" + provider)
        order_set.add(name)

        if fmt == "glyf":
            glyph = build_glyf_glyph(ds, transform, advance)
            insert_glyf(font, name, glyph, advance)
        else:
            charstring = build_cff_charstring(ds, transform, advance)
            insert_cff(font, name, charstring, advance)

        register_glyph_order(font, name)
        add_to_cmaps(font, codepoint, name)
        added += 1

    # Keep maxp in sync with the new glyph count.
    font["maxp"].numGlyphs = len(font.getGlyphOrder())

    orig_glyph_count = len(order_set) - added
    rename_font(font, args.name_suffix)

    font.save(args.out)
    size = os.path.getsize(args.out)

    print("=== patch-terminal-font summary ===")
    print("base format:        %s" % fmt)
    print("upem:               %d" % upem)
    print("advance used:       %d" % advance)
    print("cap height used:    %d" % cap_height)
    print("scale (per svg u):  %.4f" % scale)
    print("original glyphs:    %d" % orig_glyph_count)
    print("glyphs added:       %d" % added)
    print("output:             %s (%d bytes)" % (args.out, size))


if __name__ == "__main__":
    main()
