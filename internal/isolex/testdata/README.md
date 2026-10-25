`sample.ilx` and `sample.ir.golden.json` are isolex's own parser fixture
and its IR golden, copied unmodified from isolex at tag `0.3.0` (commit
`6632d01ac0`, `tests/fixtures/`). They are the work of Axonibyte
Innovations, LLC, under the Apache License 2.0 (`../NOTICE`). The module
wasm.lock pins must compile the one to the other byte for byte; when the
pin moves, copy them again from the new tag.

`sample.svg.golden` is isolex's SVG golden for the same sample, from the
same tag: every icon as `isolex render` draws it with its built-in
palette, one after another in file-name order. Package svg must draw the
IR golden to exactly this.
