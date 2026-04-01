#!/usr/bin/env bash
set -euo pipefail

echo "==> go test ./..."
go test ./...

echo "==> go vet ./..."
go vet ./...

echo "CI checks passed."
