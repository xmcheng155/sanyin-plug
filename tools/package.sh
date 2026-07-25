#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-$(date '+%Y%m%d')}"
DIST_DIR="$ROOT_DIR/dist"
ARCHIVE="$DIST_DIR/sanyin-airplay-toolkit-$VERSION.zip"

command -v zip >/dev/null 2>&1 || { echo "错误：未找到 zip" >&2; exit 1; }

mkdir -p "$DIST_DIR"
rm -f "$ARCHIVE"

(
    cd "$ROOT_DIR"
    zip -q -r "$ARCHIVE" \
        README.md \
        .gitignore \
        docs \
        scripts \
        tools \
        -x '*/__pycache__/*' '*.pyc' '.DS_Store'
)

(
    cd "$DIST_DIR"
    shasum -a 256 "$(basename "$ARCHIVE")" > "$(basename "$ARCHIVE").sha256"
)

echo "便携包已生成：$ARCHIVE"
echo "校验文件：$ARCHIVE.sha256"
echo "注意：该包不包含 NAND 备份；请单独、安全地保管 backups/。"
