#!/usr/bin/env python3
"""Decode the barcodes in every rendered label golden with zxing-cpp.

The goldens are written by `go test ./internal/render -update`, together with
testdata/barcodes.json listing what each PNG's barcodes must decode to. This
checks that layout (module snapping, quiet zones, 1-bit conversion) keeps the
symbols scannable, independently of the encoder goldens. A label that prints
mirrored is mirrored back first, as its codes are read through the glass.
"""

import json
import pathlib

import zxingcpp
from PIL import Image

ROOT = pathlib.Path(__file__).resolve().parents[2]
TESTDATA = ROOT / "internal/render/testdata"


def main():
    manifest = json.loads((TESTDATA / "barcodes.json").read_text(encoding="utf-8"))
    for entry in manifest:
        img = Image.open(TESTDATA / entry["file"]).convert("L")
        mirror = entry.get("mirror")
        if mirror in ("across", "both"):
            img = img.transpose(Image.Transpose.FLIP_LEFT_RIGHT)
        if mirror in ("down", "both"):
            img = img.transpose(Image.Transpose.FLIP_TOP_BOTTOM)
        found = sorted(r.text for r in zxingcpp.read_barcodes(img))
        want = sorted(entry["payloads"])
        if found != want:
            raise SystemExit(f"{entry['file']}: decoded {found}, want {want}")
        print(f"{entry['file']}: {found}")
    print(f"verified {len(manifest)} rendered labels")


if __name__ == "__main__":
    main()
