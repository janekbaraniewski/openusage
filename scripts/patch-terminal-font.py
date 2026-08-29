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

# Make the shared helper module importable regardless of cwd.
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from fontTools.ttLib import TTFont

from _iconfont_common import (
    INK_FILL,
    extract_path_ds,
    ink_transform,
    record_svg,
    replay_cff,
    replay_quadratic,
)


def repo_root():
    # scripts/ lives directly under the repo root.
    return os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


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


# Cap horizontal growth: tall logos may exceed the advance, but never by more
# than this factor, to avoid heavy overlap into neighboring cells.
MAX_WIDTH_FACTOR = 2.0


def add_unique_name(order_set, base_name):
    name = base_name
    i = 1
    while name in order_set:
        name = "%s_%d" % (base_name, i)
        i += 1
    return name


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


def _ps_suffix(suffix):
    # PostScript / CFF names contain no spaces and no leading '+'.
    return "-" + suffix.strip().replace(" ", "").lstrip("+")


def _compact_suffix(suffix):
    # Suffix variant with spaces stripped (used for nameID 3 unique ID and CFF).
    return suffix.strip().replace(" ", "")


def rename_font(font, suffix):
    """Rename the font family so the patched copy coexists with the original.

    Updates the 'name' table and, for CFF/OTF inputs, the CFF Top DICT names too
    (so CFF-aware consumers also see the renamed family). All updates are
    idempotent: re-running never double-appends the suffix.
    """
    name_table = font["name"]
    ps_suffix = _ps_suffix(suffix)
    compact = _compact_suffix(suffix)

    for rec in name_table.names:
        nid = rec.nameID
        cur = rec.toUnicode()
        if nid in (1, 4, 16):
            if not cur.endswith(suffix):
                name_table.setName(cur + suffix, nid, rec.platformID,
                                   rec.platEncID, rec.langID)
        elif nid == 6:
            if not cur.endswith(ps_suffix):
                name_table.setName(cur + ps_suffix, nid, rec.platformID,
                                   rec.platEncID, rec.langID)
        elif nid == 3:
            if not cur.endswith(compact):
                name_table.setName(cur + compact, nid, rec.platformID,
                                   rec.platEncID, rec.langID)

    # For CFF/OTF inputs, also rename the CFF Top DICT names so the patched font
    # does not collide with the original in CFF-aware consumers.
    if "CFF " in font:
        cff = font["CFF "].cff
        # The font name in the CFF name INDEX (PostScript-style, no spaces).
        if cff.fontNames:
            cur = cff.fontNames[0]
            if not cur.endswith(ps_suffix):
                cff.fontNames[0] = cur + ps_suffix
        top_dict = cff[cff.fontNames[0]]
        # FullName / FamilyName carry spaces; Weight does not. Append the human
        # suffix to the first two, the compact suffix to Weight.
        for attr, suff in (("FullName", suffix),
                           ("FamilyName", suffix),
                           ("Weight", compact)):
            cur = getattr(top_dict, attr, None)
            if cur and not cur.endswith(suff):
                setattr(top_dict, attr, cur + suff)


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
                    default=os.path.join(repo_root(), "internal", "tmux", "assets", "icons"))
    args = ap.parse_args()

    font = TTFont(args.base)
    fmt = detect_format(font)
    upem = font["head"].unitsPerEm
    advance = choose_advance(font, upem)
    # Icons are scaled per-glyph by their ink bbox so they fill the line height.
    # The target band is the WHOLE line (ascent down to descent), and each glyph
    # is centered vertically in that band, so icons fill the cell top-to-bottom
    # and sit centered rather than floating in the ascender region.
    ascent = font["hhea"].ascent
    descent = font["hhea"].descent  # negative
    line_h = ascent - descent

    with open(args.manifest) as fh:
        manifest = json.load(fh)

    order_set = set(font.getGlyphOrder())
    added = 0
    heights = []  # (name, glyph-height-in-font-units) for reporting

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

        rec = record_svg(ds)
        src_label = "%s (%s.svg)" % (provider, svg)
        try:
            transform, scaled_w, scaled_h = ink_transform(
                rec,
                target_h=INK_FILL * line_h,
                box_w=advance,
                box_h=line_h,
                box_y0=descent,
                max_w=advance * MAX_WIDTH_FACTOR,
                src_label=src_label,
            )
        except ValueError as exc:
            print("warn: %s, skipping %s" % (exc, provider), file=sys.stderr)
            continue

        if fmt == "glyf":
            glyph = replay_quadratic(rec, transform)
            insert_glyf(font, name, glyph, advance)
        else:
            charstring = replay_cff(rec, transform, advance)
            insert_cff(font, name, charstring, advance)

        order_set.add(name)
        register_glyph_order(font, name)
        add_to_cmaps(font, codepoint, name)
        heights.append((name, round(scaled_h)))
        added += 1

    if added == 0:
        print("error: no glyphs were added; output not written", file=sys.stderr)
        return 1

    # Keep maxp in sync with the new glyph count.
    font["maxp"].numGlyphs = len(font.getGlyphOrder())

    orig_glyph_count = len(order_set) - added
    rename_font(font, args.name_suffix)

    font.save(args.out)
    size = os.path.getsize(args.out)

    target_h = INK_FILL * line_h
    print("=== patch-terminal-font summary ===")
    print("base format:        %s" % fmt)
    print("upem:               %d" % upem)
    print("advance used:       %d" % advance)
    print("base ascender:      %d" % ascent)
    print("base descender:     %d" % descent)
    print("line height:        %d (ascent - descent)" % line_h)
    print("target ink height:  %.0f (%.0f%% of line height)" % (target_h, INK_FILL * 100))
    print("original glyphs:    %d" % orig_glyph_count)
    print("glyphs added:       %d" % added)
    print("output:             %s (%d bytes)" % (args.out, size))
    print("augmented glyph ink heights (font units):")
    for name, h in heights:
        print("    - %-22s height=%d" % (name, h))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
