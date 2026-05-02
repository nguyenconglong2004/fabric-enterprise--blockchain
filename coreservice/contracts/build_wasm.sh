#!/usr/bin/env bash
# Build WASM contracts (TinyGo). WASI target = works with wazero + wasi_snapshot_preview1.
# Plain -target wasm pulls in gojs (browser runtime) and breaks: module[gojs] not instantiated.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
if ! command -v tinygo >/dev/null 2>&1; then
  echo "Install TinyGo: https://tinygo.org/getting-started/install/"
  exit 1
fi
cd "$ROOT"
for dir in example_asset demo_inventory; do
  echo "==> Building $dir"
  tinygo build -o "$dir/my_contract.wasm" -target wasi -no-debug -scheduler=none "./$dir"
done
echo "Done."
