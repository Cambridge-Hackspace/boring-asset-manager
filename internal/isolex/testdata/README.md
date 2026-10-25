`sample.ilx` and `sample.ir.golden.json` are isolex's own parser fixture
and its IR golden, copied unmodified from isolex at tag `0.3.0` (commit
`6632d01ac0`, `tests/fixtures/`), Apache-2.0. The module wasm.lock pins
must compile the one to the other byte for byte; when the pin moves,
copy them again from the new tag.
