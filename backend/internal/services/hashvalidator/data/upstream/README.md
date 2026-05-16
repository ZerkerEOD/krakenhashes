# Vendored Upstream: HashPals/Name-That-Hash

This directory contains a verbatim copy of `name_that_hash/hashes.py` from
[HashPals/Name-That-Hash](https://github.com/HashPals/Name-That-Hash), which
KrakenHashes uses as the source of hash-format regex patterns for the hash
validator (see GitHub issue #38).

## Pinned upstream

- **Repository**: https://github.com/HashPals/Name-That-Hash
- **File**: `name_that_hash/hashes.py`
- **Pinned commit**: `4717bf38e9ec6b86d3003d0fc0554d034ccccb8b`
- **Vendored on**: 2026-05-15

## License

Name-That-Hash is licensed under the **GNU General Public License v3.0**.
The full text is in [`LICENSE`](./LICENSE).

KrakenHashes is licensed under the GNU Affero General Public License v3.0
(AGPL-3.0), which is compatible with GPL-3.0. The combined work is
distributed under AGPL-3.0. Attribution is preserved per GPL-3.0 §5.

## Updating

To bump to a newer upstream version:

1. Update the pinned commit hash above.
2. Replace `hashes.py` with the new upstream copy.
3. Re-run the generator: `go run ./backend/scripts/gen_hash_patterns`
4. Review the diff in `../patterns.go` and run the validator unit tests.

Do not edit `hashes.py` in-place. Any local fixups belong in the generator
script (`backend/scripts/gen_hash_patterns/main.go`) or in
`backend/internal/services/hashvalidator/structural/` overrides.
