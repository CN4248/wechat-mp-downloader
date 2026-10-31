#!/usr/bin/env bash
# 一键构建并启动 WebUI 容器（端口 2178，宿主机目录挂载到 /download）。
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v docker >/dev/null 2>&1; then
  echo "错误：未找到 docker，请先安装 Docker。" >&2
  exit 1
fi

if [ ! -f .env ]; then
  cp .env.example .env
  echo "已生成 .env（可按需修改 DOWNLOAD_DIR / WEB_PORT）。"
fi

# shellcheck disable=SC1091
set -a; . ./.env; set +a
DOWNLOAD_DIR="${DOWNLOAD_DIR:-/mnt/media/wechat}"
WEB_PORT="${WEB_PORT:-2178}"

if [ ! -x build/wxmp-linux-amd64 ] || [ -n "$(find cmd internal -newer build/wxmp-linux-amd64 -type f -print -quit 2>/dev/null)" ]; then
  echo "==> 编译静态二进制"
  ./scripts/build.sh
fi

mkdir -p "$DOWNLOAD_DIR"
echo "==> 构建镜像并启动容器"
if docker compose version >/dev/null 2>&1; then
  docker compose up -d --build
else
  docker-compose up -d --build
fi

echo "==> WebUI: http://127.0.0.1:${WEB_PORT}"
echo "==> 宿主机下载目录: ${DOWNLOAD_DIR}"
