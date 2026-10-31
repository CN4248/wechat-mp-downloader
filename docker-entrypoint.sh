#!/bin/sh
set -e
mkdir -p /download 2>/dev/null || true
exec /usr/local/bin/wxmp "$@"
