#!/usr/bin/env bash
# Deprecated stub — use coreservice/contracts/build_wasm.sh
#   cd coreservice/contracts && ./build_wasm.sh <name>
exec "$(cd "$(dirname "$0")" && pwd)/../coreservice/contracts/build_wasm.sh" "$@"
