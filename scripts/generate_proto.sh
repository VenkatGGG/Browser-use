#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${ROOT_DIR}/internal/gen"

mkdir -p "${OUT_DIR}"

protoc \
  --proto_path="${ROOT_DIR}/proto" \
  --go_out="${OUT_DIR}" \
  --go_opt=paths=source_relative \
  --go-grpc_out="${OUT_DIR}" \
  --go-grpc_opt=paths=source_relative \
  "${ROOT_DIR}/proto/node.proto"

echo "generated protobuf stubs into ${OUT_DIR}"
