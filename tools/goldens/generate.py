#!/usr/bin/env python3
"""Generate QR Code and Micro QR Code golden matrices for the Go encoders.

segno is the reference encoder. Each symbol is also rendered and decoded with
zxing-cpp, so a golden is only written once an independent reader agrees it
carries the payload. Run it through the pinned container (see README.md) so
the output is reproducible.
"""

import json
import pathlib
import random

import segno
import zxingcpp
from PIL import Image
from segno import consts, encoder

ROOT = pathlib.Path(__file__).resolve().parents[2]
ALNUM = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:"

# --- Bring segno 1.6.6 in line with ISO/IEC 18004:2015 7.4.10 ----------------
#
# 1. When the stream is already on a codeword boundary after the terminator,
#    segno appends a whole zero codeword instead of moving straight on to the
#    pad codewords.
# 2. For M1 and M3 it fills the remaining capacity with zero bits, where the
#    standard alternates 0xEC/0x11 pad codewords and only the final four-bit
#    codeword is 0000.
#
# Neither affects whether a symbol scans, but the BAM encoders follow the
# standard, and the goldens must too.

_PAD_CODEWORDS = ((1, 1, 1, 0, 1, 1, 0, 0), (0, 0, 0, 1, 0, 0, 0, 1))


def _write_padding_bits(buff, version, length):
    if version not in (consts.VERSION_M1, consts.VERSION_M3) and length % 8:
        buff.extend([0] * (8 - length % 8))


def _write_pad_codewords(buff, version, capacity, length):
    full = capacity - capacity % 8
    if length < full:
        buff.extend([0] * (-length % 8))
        for i in range((full - len(buff)) // 8):
            buff.extend(_PAD_CODEWORDS[i % 2])
    buff.extend([0] * (capacity - len(buff)))


encoder.write_padding_bits = _write_padding_bits
encoder.write_pad_codewords = _write_pad_codewords


def mode_of(payload: bytes) -> str:
    """The single-segment mode the Go encoders choose."""
    text = payload.decode("latin-1")
    if all(c in "0123456789" for c in text):
        return "numeric"
    if all(c in ALNUM for c in text):
        return "alphanumeric"
    return "byte"


def decodes(code, payload: bytes, border: int) -> bool:
    rows = code.matrix
    size = len(rows)
    scale = 6
    img = Image.new("L", ((size + 2 * border) * scale,) * 2, 255)
    px = img.load()
    for y, row in enumerate(rows):
        for x, dark in enumerate(row):
            if dark:
                for dy in range(scale):
                    for dx in range(scale):
                        px[(x + border) * scale + dx, (y + border) * scale + dy] = 0
    result = zxingcpp.read_barcode(img)
    return result is not None and result.bytes == payload


def golden(name, payload: bytes, level, version, mask, micro):
    code = segno.make(
        payload,
        error=level,
        version=version,
        mode=mode_of(payload),
        mask=mask,
        micro=micro,
        boost_error=False,
    )
    if not decodes(code, payload, 2 if micro else 4):
        raise SystemExit(f"{name}: zxing-cpp could not decode the segno symbol")
    got_version = code.version
    if micro:
        got_version = int(code.designator[1])
    return {
        "name": name,
        "payload": payload.decode("utf-8"),
        "level": level,
        "version": 0 if version is None else (int(version[1]) if micro else version),
        "mask": -1 if mask is None else mask,
        "want_version": got_version,
        "want_mask": code.mask,
        "rows": ["".join("1" if m else "0" for m in row) for row in code.matrix],
    }


def byte_capacity(version, level):
    error = {"L": consts.ERROR_LEVEL_L, "M": consts.ERROR_LEVEL_M,
             "Q": consts.ERROR_LEVEL_Q, "H": consts.ERROR_LEVEL_H}[level]
    count_bits = 8 if version < 10 else 16
    return (consts.SYMBOL_CAPACITY[version][error] - 4 - count_bits) // 8


def qr_cases():
    rng = random.Random(0xBA11)
    cases = []
    for i, level in enumerate("LMQH"):
        for mask in range(8):
            if mask % 4 == i:  # two masks per level, all eight across levels
                cases.append((f"numeric-1{level}-mask{mask}", b"01234567", level, 1, mask))
    cases += [
        ("alphanumeric-hello", b"HELLO WORLD", "Q", 1, 2),
        ("byte-aligned-terminator", b"A", "L", 1, 3),
        ("url-auto", b"https://bam.example/a/7c9e6679-7425-40de-944b-e07fc1f90ae7", "M", None, None),
        ("utf8-auto", "café ☕ 東京".encode(), "H", None, None),
        # Character count indicators widen at versions 10 and 27.
        ("numeric-v10-plus", b"31415926535897932384626433832795" * 20, "M", None, None),
        ("numeric-v27-plus", b"27182818284590452353602874713526" * 95, "M", None, None),
        ("alphanumeric-v27-plus", (ALNUM * 50).encode(), "L", None, None),
        ("numeric-auto-mask", b"8675309", "L", None, None),
        ("alphanumeric-auto-mask", b"BAM-0001", "H", None, None),
    ]
    # Pinned versions covering every alignment layout family, version
    # information (7+), each remainder-bit count and multi-block interleaving.
    for v in (2, 5, 6, 7, 9, 10, 14, 20, 21, 26, 27, 28, 32, 34, 35, 40):
        level = "LMQH"[v % 4]
        n = byte_capacity(v, level) - (0 if v % 2 == 0 else 5)
        payload = bytes(rng.choice(b"abcdefghijklmnopqrstuvwxyz0123456789-/:.") for _ in range(n))
        cases.append((f"byte-v{v}-{level}", payload, level, v, v % 8))
    return [golden(*c, micro=False) for c in cases]


def micro_cases():
    cases = []
    for version, levels in (("M2", "LM"), ("M3", "LM"), ("M4", "LMQ")):
        for level in levels:
            for mask in range(4):
                payload = b"12345" if version == "M2" else b"BAM-42"
                cases.append((f"{version}-{level}-mask{mask}", payload, level, version, mask))
    cases += [
        # M3-L: 23 digits fill all 84 data bits; 22 end inside the final
        # four-bit codeword; 15 leave room for pad codewords.
        ("M3-L-full", b"12345678901234567890123", "L", "M3", None),
        ("M3-L-tail", b"1234567890123456789012", "L", "M3", None),
        ("M3-L-pads", b"123456789012345", "L", "M3", None),
        ("M3-M-alphanumeric", b"BAM 7", "M", "M3", None),
        ("M4-L-numeric-max", b"9" * 35, "L", None, None),
        ("M4-L-alphanumeric-max", b"ABCDEFGHIJKLMNOPQRSTU", "L", None, None),
        ("M4-Q-auto", b"BAM-0001", "Q", None, None),
        ("auto-smallest", b"7", "L", None, None),
        ("auto-alphanumeric", b"A1", "M", None, None),
    ]
    return [golden(*c, micro=True) for c in cases]


def write(path, cases):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(cases, indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"wrote {len(cases)} cases to {path.relative_to(ROOT)}")


if __name__ == "__main__":
    write(ROOT / "internal/generators/qr/testdata/segno.json", qr_cases())
    write(ROOT / "internal/generators/microqr/testdata/segno.json", micro_cases())
