#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-}"
PRIVATE_KEY="$ROOT_DIR/.tools/update-signing-key"
PUBLIC_KEY="$ROOT_DIR/dist/update-public-key"
OUTPUT="$ROOT_DIR/dist/sanyin-plug-${VERSION}.sanyin-update"

[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
	printf '用法：npm run package:update -- X.Y.Z\n' >&2
	exit 2
}

export GOPATH="$ROOT_DIR/.cache/go-path"
export GOMODCACHE="$GOPATH/pkg/mod"
export GOCACHE="$ROOT_DIR/.cache/go-build"
mkdir -p "$GOCACHE" "$GOMODCACHE" "$ROOT_DIR/dist"
go run "$ROOT_DIR/service/cmd/sanyin-update" keygen \
	-private "$PRIVATE_KEY" \
	-public "$PUBLIC_KEY"

SANYIN_VERSION="$VERSION" "$ROOT_DIR/tools/build-service.sh" armv7

go run "$ROOT_DIR/service/cmd/sanyin-update" package \
	-private "$PRIVATE_KEY" \
	-binary "$ROOT_DIR/dist/sanyin-config-linux-armv7" \
	-version "$VERSION" \
	-output "$OUTPUT"

printf '下一步：首次启用时通过 ADB/SSH 运行 sanyinctl config-install，将公钥安装到音箱。\n'
