#!/bin/sh
set -u

PROGRAM=/mnt/UDISK/sanyin-config/sanyin-config
CANDIDATE=/mnt/UDISK/sanyin-config/update/sanyin-config.candidate
PREVIOUS=/mnt/UDISK/sanyin-config/sanyin-config.previous
FAILED=/mnt/UDISK/sanyin-config/sanyin-config.failed
STATUS=/mnt/UDISK/sanyin-config/update/update-status
LOCK=/tmp/sanyin-update.lock
SERVICE=/etc/init.d/sanyin_config

read_status_value() {
	sed -n "s/^$1=//p" "$STATUS" 2>/dev/null | head -n 1
}

write_status() {
	state="$1"
	version="$2"
	message="$3"
	updated_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date)"
	temp="${STATUS}.tmp"
	{
		printf 'state=%s\n' "$state"
		printf 'version=%s\n' "$version"
		printf 'message=%s\n' "$message"
		printf 'updated_at=%s\n' "$updated_at"
	} >"$temp"
	chmod 0600 "$temp"
	mv "$temp" "$STATUS"
}

service_healthy() {
	ps w | grep -q "[/]mnt/UDISK/sanyin-config/sanyin-config.*-mode device" || return 1
	netstat -lnt 2>/dev/null | grep -q ":8787 " || return 1
	return 0
}

if ! mkdir "$LOCK" 2>/dev/null; then
	exit 1
fi
trap 'rmdir "$LOCK" 2>/dev/null || true' EXIT

version="$(read_status_value version)"
sleep 2

if [ ! -x "$CANDIDATE" ] || [ ! -x "$PROGRAM" ] || [ ! -x "$SERVICE" ]; then
	write_status failed "$version" "候选程序、当前程序或启动服务缺失"
	exit 1
fi

write_status applying "$version" "正在替换程序并重启服务"
rm -f "$PREVIOUS" "$FAILED"
if ! mv "$PROGRAM" "$PREVIOUS" || ! mv "$CANDIDATE" "$PROGRAM"; then
	[ -x "$PREVIOUS" ] && mv "$PREVIOUS" "$PROGRAM"
	write_status failed "$version" "替换程序失败，已保留当前版本"
	exit 1
fi
chmod 0755 "$PROGRAM"

actual_version="$("$PROGRAM" -version 2>/dev/null | sed -n '1p')"
if [ "$actual_version" != "sanyin-config $version" ]; then
	mv "$PROGRAM" "$FAILED" 2>/dev/null || true
	mv "$PREVIOUS" "$PROGRAM" 2>/dev/null || true
	chmod 0755 "$PROGRAM" 2>/dev/null || true
	write_status failed "$version" "候选程序内置版本与更新清单不一致，已保留上一版"
	exit 1
fi

"$SERVICE" restart >/tmp/sanyin_update_restart.log 2>&1 || true
attempt=0
while [ "$attempt" -lt 20 ]; do
	if service_healthy; then
		write_status succeeded "$version" "新版本已启动并通过端口健康检查"
		exit 0
	fi
	attempt=$((attempt + 1))
	sleep 1
done

"$SERVICE" stop >/dev/null 2>&1 || true
mv "$PROGRAM" "$FAILED" 2>/dev/null || true
if ! mv "$PREVIOUS" "$PROGRAM"; then
	write_status rollback_failed "$version" "新版本启动失败，且无法恢复上一版"
	exit 1
fi
chmod 0755 "$PROGRAM"
"$SERVICE" restart >/tmp/sanyin_update_rollback.log 2>&1 || true

attempt=0
while [ "$attempt" -lt 20 ]; do
	if service_healthy; then
		write_status rolled_back "$version" "新版本健康检查失败，已自动恢复上一版"
		exit 1
	fi
	attempt=$((attempt + 1))
	sleep 1
done

write_status rollback_failed "$version" "已恢复上一版文件，但服务健康检查仍失败；请通过 SSH 或 ADB 检查"
exit 1
