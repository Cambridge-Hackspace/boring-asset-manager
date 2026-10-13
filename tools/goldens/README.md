# Barcode goldens

Reference data for the encoders in `internal/generators`. The Go tests only read
the checked-in files, so neither Python nor this container is needed to run
them.

| Script | Produces / checks |
|---|---|
| `generate.py` | `internal/generators/qr/testdata/segno.json` and `internal/generators/microqr/testdata/segno.json`: module matrices from [segno](https://github.com/heuer/segno), each also decoded with [zxing-cpp](https://github.com/zxing-cpp/zxing-cpp) before it is written. |

`generate.py` patches two places where segno 1.6.6 pads the data stream
differently from ISO/IEC 18004:2015 7.4.10. The script explains both. Neither
affects whether a symbol scans.

## Running

From the repository root, with podman (docker works the same way):

```sh
podman build -t bam-goldens -f tools/goldens/Containerfile tools/goldens
podman run --rm -v "$PWD:/src" -w /src bam-goldens python tools/goldens/generate.py
```

The image pins Python, segno, zxing-cpp and Pillow, so regenerating with
unchanged cases reproduces the files byte for byte.
