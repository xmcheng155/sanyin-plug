#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEFAULT_ADB="$ROOT_DIR/.tools/platform-tools/adb"
ADB_BIN="${ADB:-$DEFAULT_ADB}"
SERIAL="${SANYIN_SERIAL:-}"
DURATION=40
LABEL=""

usage() {
    cat <<'EOF'
用法：tools/sanyin_protocol_capture.sh --label LABEL [选项]

只读采集设备会话 D-Bus、相关服务增量日志以及操作前后状态。

选项：
  --label LABEL      本次单动作标签，仅允许字母、数字、点、下划线和连字符
  --duration SEC     采集秒数，默认 40，范围 5-300
  --serial SERIAL    指定 ADB 设备
  --adb PATH         指定 adb
  --help, -h         显示帮助

示例：
  ./tools/sanyin_protocol_capture.sh --label bluetooth-open --duration 40

原始数据保存在 .tools/protocol-captures/，可能包含设备和网络标识；
同目录的 *.redacted.log 才适合用于分析记录或分享。
EOF
}

die() {
    printf '[错误] %s\n' "$*" >&2
    exit 1
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --label)
            [[ $# -ge 2 ]] || die "--label 需要参数"
            LABEL="$2"
            shift 2
            ;;
        --duration)
            [[ $# -ge 2 ]] || die "--duration 需要参数"
            DURATION="$2"
            shift 2
            ;;
        --serial)
            [[ $# -ge 2 ]] || die "--serial 需要参数"
            SERIAL="$2"
            shift 2
            ;;
        --adb)
            [[ $# -ge 2 ]] || die "--adb 需要参数"
            ADB_BIN="$2"
            shift 2
            ;;
        --help|-h)
            usage
            exit 0
            ;;
        *)
            die "未知参数：$1"
            ;;
    esac
done

[[ -n "$LABEL" ]] || die "必须提供 --label"
[[ "$LABEL" =~ ^[A-Za-z0-9._-]+$ ]] || die "--label 包含不允许的字符"
[[ "$DURATION" =~ ^[0-9]+$ ]] || die "--duration 必须是整数"
((DURATION >= 5 && DURATION <= 300)) || die "--duration 必须在 5-300 秒之间"
[[ -x "$ADB_BIN" ]] || die "ADB 不可执行：$ADB_BIN"

if [[ -z "$SERIAL" ]]; then
    SERIAL="$($ADB_BIN devices | awk 'NR > 1 && $2 == "device" {print $1; exit}')"
fi
[[ -n "$SERIAL" ]] || die "没有发现已授权的 ADB 设备"

$ADB_BIN -s "$SERIAL" shell 'test -s /tmp/dbus_env.sh && test -x /usr/bin/dbus-monitor' \
    >/dev/null 2>&1 || die "设备缺少 D-Bus 环境或 dbus-monitor"

STAMP="$(date '+%Y%m%d-%H%M%S')"
CAPTURE_DIR="$ROOT_DIR/.tools/protocol-captures/$STAMP-$LABEL"
mkdir -p "$CAPTURE_DIR"
chmod 0700 "$CAPTURE_DIR"

DBUS_RAW="$CAPTURE_DIR/dbus.raw.log"
SERVICE_RAW="$CAPTURE_DIR/services.raw.log"
STATE_BEFORE="$CAPTURE_DIR/state-before.raw.log"
STATE_AFTER="$CAPTURE_DIR/state-after.raw.log"

snapshot_state() {
    local destination="$1"
    $ADB_BIN -s "$SERIAL" shell '
        date -R
        ps w | grep -E "[a]pp_nevsps_bt|[a]pp_wifi_manager|[n]etease_control_center|[KkSs]Player|[a]larmer"
        netstat -lntp 2>/dev/null
        hciconfig -a 2>/dev/null
        iwconfig wlan0 2>/dev/null
    ' >"$destination" 2>&1
}

monitor_pid=""
log_pid=""
cleanup() {
    if [[ -n "$monitor_pid" ]]; then
        kill "$monitor_pid" >/dev/null 2>&1 || true
        wait "$monitor_pid" >/dev/null 2>&1 || true
    fi
    if [[ -n "$log_pid" ]]; then
        kill "$log_pid" >/dev/null 2>&1 || true
        wait "$log_pid" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT INT TERM

snapshot_state "$STATE_BEFORE"

$ADB_BIN -s "$SERIAL" shell \
    '. /tmp/dbus_env.sh && exec dbus-monitor --session interface=netease.ihw.SmartAudio' \
    >"$DBUS_RAW" 2>&1 &
monitor_pid=$!

REMOTE_LOGS="$($ADB_BIN -s "$SERIAL" shell '
    find /tmp -maxdepth 1 -type f 2>/dev/null |
        grep -E "/(app_nevsps_bt|app_wifi_manager|netease_control_center|netease_voice|alarmer|ihwplayer|KPlayer|SPlayer)_[0-9]+[.]log$"
    ' | tr -d '\r' | tr '\n' ' ')"
if [[ -n "$REMOTE_LOGS" ]]; then
    $ADB_BIN -s "$SERIAL" shell "exec tail -n 0 -f $REMOTE_LOGS" >"$SERVICE_RAW" 2>&1 &
    log_pid=$!
fi

printf '[信息] 已开始采集：%s\n' "$CAPTURE_DIR"
printf '[信息] 请只执行动作“%s”；%s 秒后自动结束。\n' "$LABEL" "$DURATION"
sleep "$DURATION"
cleanup
monitor_pid=""
log_pid=""

snapshot_state "$STATE_AFTER"
chmod 0600 "$CAPTURE_DIR"/*.raw.log

for source in "$CAPTURE_DIR"/*.raw.log; do
    destination="${source%.raw.log}.redacted.log"
    python3 "$ROOT_DIR/tools/redact_protocol_capture.py" "$source" "$destination"
    chmod 0600 "$destination"
done

printf '[信息] 采集完成：%s\n' "$CAPTURE_DIR"
printf '[信息] 分析或分享时仅使用 *.redacted.log。\n'
