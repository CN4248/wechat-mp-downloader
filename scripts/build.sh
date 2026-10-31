#!/usr/bin/env bash
# 构建 wxmp 的 Linux（容器用）与 Windows 可执行文件。
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${VERSION:-1.2.2}"
mkdir -p build dist
export CGO_ENABLED=0

echo "==> gofmt / vet"
test -z "$(gofmt -l cmd internal)" || { gofmt -l cmd internal; echo "请先 gofmt -w"; exit 1; }
go vet ./...

echo "==> linux/amd64 (Docker)"
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
  -o build/wxmp-linux-amd64 ./cmd/wxmp

echo "==> windows/amd64 (.exe)"
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
  -o "dist/wxmp-windows-amd64.exe" ./cmd/wxmp

echo "==> 完成"
ls -lh build/wxmp-linux-amd64 "dist/wxmp-windows-amd64.exe"
