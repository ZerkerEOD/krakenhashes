# Third-Party Notices — hashvalidator

The hash validator package incorporates regex patterns derived from the
following third-party project. Their license terms are preserved alongside
the KrakenHashes AGPL-3.0 license.

## HashPals/Name-That-Hash

- **Project**: Name-That-Hash
- **Upstream**: https://github.com/HashPals/Name-That-Hash
- **Pinned commit**: `4717bf38e9ec6b86d3003d0fc0554d034ccccb8b`
- **License**: GNU General Public License v3.0 (see [upstream/LICENSE](./upstream/LICENSE))

The regex patterns in [`patterns.go`](./patterns.go) are mechanically derived
from `name_that_hash/hashes.py` (vendored in [upstream/hashes.py](./upstream/hashes.py))
by the generator at `backend/scripts/gen_hash_patterns/main.go`.

Per GPL-3.0 §5, the combined work distributed in KrakenHashes is licensed
under AGPL-3.0 (GPL-3.0 compatible). Source code for both the upstream
library and KrakenHashes' derived data is available in this repository.
