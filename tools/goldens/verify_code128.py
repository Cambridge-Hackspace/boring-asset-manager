#!/usr/bin/env python3
"""Decode every Code 128 golden with zxing-cpp and check it yields its payload.

Code 128 has more than one valid encoding for most payloads, so the goldens
are produced by the Go encoder (go test ./internal/generators/code128 -update)
and checked here with an independent reader instead of compared with another
encoder.
"""

import json
import pathlib

import zxingcpp
from PIL import Image

ROOT = pathlib.Path(__file__).resolve().parents[2]
QUIET_ZONE = 10
SCALE = 3
HEIGHT = 60


def main():
    path = ROOT / "internal/generators/code128/testdata/golden.json"
    cases = json.loads(path.read_text(encoding="utf-8"))
    for case in cases:
        modules = case["modules"]
        width = (len(modules) + 2 * QUIET_ZONE) * SCALE
        img = Image.new("L", (width, HEIGHT), 255)
        px = img.load()
        for i, bar in enumerate(modules):
            if bar == "1":
                for dx in range(SCALE):
                    for y in range(HEIGHT):
                        px[(QUIET_ZONE + i) * SCALE + dx, y] = 0
        result = zxingcpp.read_barcode(img, formats=zxingcpp.BarcodeFormat.Code128)
        if result is None or result.text != case["payload"]:
            got = None if result is None else result.text
            raise SystemExit(f"{case['payload']!r}: decoded as {got!r}")
    print(f"verified {len(cases)} Code 128 goldens")


if __name__ == "__main__":
    main()
