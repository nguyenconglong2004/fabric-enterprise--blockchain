#!/usr/bin/env bash
# Build one WASM contract (TinyGo + schema.json).
# Usage:
#   ./build_wasm.sh escrow
#   ./build_wasm.sh loyalty_xfer
#   ./build_wasm.sh --all          # mọi contract mẫu trong repo
#
# Output (sẵn để deploy):
#   <name>/<name>.wasm
#   <name>/schema.json
#
# Deploy ví dụ:
#   curl -X POST http://127.0.0.1:8080/api/tx/deploy \
#     -F "contract_name=escrow" \
#     -F "file=@escrow/escrow.wasm" \
#     -F "schema=@escrow/schema.json"
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
CORE="$(cd "$ROOT/.." && pwd)"

ALL_CONTRACTS=(example_asset demo_inventory bench_ping transfer double_credit escrow loyalty_xfer qty_credit)

usage() {
  echo "Usage: $0 <contract_name>"
  echo "       $0 --all"
  echo ""
  echo "Known: ${ALL_CONTRACTS[*]}"
  exit 1
}

if ! command -v tinygo >/dev/null 2>&1; then
  echo "Install TinyGo: https://tinygo.org/getting-started/install/"
  exit 1
fi

build_one() {
  local name="$1"
  local dir="$ROOT/$name"
  if [[ ! -d "$dir" || ! -f "$dir/main.go" ]]; then
    echo "❌ Contract not found: $dir (need main.go)"
    exit 1
  fi
  echo "==> Schema $name"
  (cd "$CORE" && go run ./cmd/gen_schema -dir "./contracts/$name")
  echo "==> Building $name → $name/$name.wasm"
  (cd "$ROOT" && tinygo build -o "$name/$name.wasm" -target wasi -no-debug -scheduler=none "./$name")
  echo "✅ $name/$name.wasm + $name/schema.json"
}

cd "$ROOT"

if [[ $# -lt 1 ]]; then
  usage
fi

case "$1" in
  -h|--help)
    usage
    ;;
  --all|-a)
    for name in "${ALL_CONTRACTS[@]}"; do
      build_one "$name"
    done
    echo "Done (all)."
    ;;
  *)
    build_one "$1"
    echo "Done."
    echo ""
    echo "Deploy:"
    echo "  curl -X POST http://127.0.0.1:8080/api/tx/deploy \\"
    echo "    -F 'contract_name=$1' \\"
    echo "    -F 'file=@$1/$1.wasm' \\"
    echo "    -F 'schema=@$1/schema.json'"
    ;;
esac
