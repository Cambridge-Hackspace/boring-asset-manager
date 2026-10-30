These are isolex's own fixtures and goldens, copied unmodified from
isolex at tag `0.6.0` (commit `b37cfd4`, `tests/fixtures/`), where its
sample, transforms and text are as they were at `0.4.0` (commit
`d0aa66f`): those three lexicons, the IR version 2 each compiles to, and
the SVG isolex's reference renderer draws from that IR with its built-in
palette; and its palette lexicon, which carries its own colors in a
`palette` block, with the SVG `isolex render` draws from them. They are
the work of Axonibyte Innovations, LLC, under the Apache License 2.0
(`../../NOTICE`).

Package svg must draw each IR golden to exactly its SVG golden:
`sample.svg.golden` and `palette.svg.golden` as `isolex render` writes
one file per icon, strung together in file-name order, and the others as
isolex's tests string each icon together in IR order, each after a
`<!-- name -->` line.
