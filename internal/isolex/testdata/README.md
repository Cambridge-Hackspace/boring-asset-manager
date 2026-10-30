`sample.ilx` and `sample.ir.golden.json` are isolex's own parser fixture
and its IR golden, copied unmodified from isolex at tag `0.3.1` (commit
`6ee4eecaf4`, `tests/fixtures/`). They are the work of Axonibyte
Innovations, LLC, under the Apache License 2.0 (`../NOTICE`). They are
kept as IR version 1, which bam still reads: a build an older isolex
made is drawn until a new one replaces it.

`isolex-0.6.0/` holds the fixtures and goldens of the release wasm.lock
pins, which the module must compile to byte for byte (see its README);
when the pin moves, copy the new tag's into a directory of its own.

`sample.svg.golden` is isolex's SVG golden for the same sample, from the
same tag: every icon as `isolex render` draws it with its built-in
palette, one after another in file-name order. Package svg must draw the
IR golden to exactly this.
