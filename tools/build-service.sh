#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET="${1:-host}"
VERSION="${SANYIN_VERSION:-}"

if [[ -z "$VERSION" ]]; then
	if git -C "$ROOT_DIR" rev-parse --short HEAD >/dev/null 2>&1; then
		VERSION="dev-$(git -C "$ROOT_DIR" rev-parse --short HEAD)"
	else
		VERSION="dev"
	fi
fi
COMMIT="$(git -C "$ROOT_DIR" rev-parse --short=12 HEAD 2>/dev/null || printf unknown)"
BUILT_AT="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.builtAt=${BUILT_AT}"
export GOPATH="$ROOT_DIR/.cache/go-path"
export GOMODCACHE="$GOPATH/pkg/mod"
export GOCACHE="$ROOT_DIR/.cache/go-build"

mkdir -p "$GOCACHE" "$GOMODCACHE" "$ROOT_DIR/dist"

case "$TARGET" in
	host)
		go build -ldflags "$LDFLAGS" -o "$ROOT_DIR/dist/sanyin-config" "$ROOT_DIR/service/cmd/sanyin-config"
		go build -ldflags "-s -w -X main.version=${VERSION}" -o "$ROOT_DIR/dist/sanyin-sshd" "$ROOT_DIR/service/cmd/sanyin-sshd"
		;;
	armv7)
		GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
			go build -ldflags "$LDFLAGS" -o "$ROOT_DIR/dist/sanyin-config-linux-armv7" "$ROOT_DIR/service/cmd/sanyin-config"
		GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
			go build -ldflags "-s -w -X main.version=${VERSION}" -o "$ROOT_DIR/dist/sanyin-sshd-linux-armv7" "$ROOT_DIR/service/cmd/sanyin-sshd"
		;;
	*)
		printf '错误：未知构建目标 %s（可选 host、armv7）\n' "$TARGET" >&2
		exit 2
		;;
esac

printf '构建完成：version=%s commit=%s builtAt=%s target=%s\n' "$VERSION" "$COMMIT" "$BUILT_AT" "$TARGET"
