These are isolex's own fixtures and goldens, copied unmodified from
isolex at tag `0.4.0` (commit `d0aa66f`, `tests/fixtures/`): its sample,
transforms and text lexicons, the IR version 2 each compiles to, and the
SVG isolex's reference renderer draws from that IR with its built-in
palette. They are the work of Axonibyte Innovations, LLC, under the
Apache License 2.0 (`../../NOTICE`).

Package svg must draw each IR golden to exactly its SVG golden:
`sample.svg.golden` as `isolex render` writes one file per icon, strung
together in file-name order, and the others as isolex's tests string
each icon together in IR order, each after a `<!-- name -->` line.
