#!/usr/bin/env bash
# Build WASM contracts (TinyGo). WASI target = works with wazero + wasi_snapshot_preview1.
# Auto-generates schema.json from type Payload in each main.go (no hand-written schema).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
CORE="$(cd "$ROOT/.." && pwd)"
if ! command -v tinygo >/dev/null 2>&1; then
  echo "Install TinyGo: https://tinygo.org/getting-started/install/"
  exit 1
fi
cd "$ROOT"
for dir in example_asset demo_inventory bench_ping transfer double_credit; do
  echo "==> Schema $dir"
  (cd "$CORE" && go run ./cmd/gen_schema -dir "./contracts/$dir")
  echo "==> Building $dir"
  tinygo build -o "$dir/my_contract.wasm" -target wasi -no-debug -scheduler=none "./$dir"
done
echo "Done."
