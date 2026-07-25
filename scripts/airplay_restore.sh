#!/bin/sh

# 三音云音箱 AirPlay 守护脚本。
# 厂商控制中心会依据云端 airPlayPrivilege=0 关闭 SPlayer；
# 本脚本只在 SPlayer 未监听时重新发送原生启动命令。

PORT=5002
CHECK_INTERVAL=20
LOG_FILE=/tmp/airplay_restore.log
DBUS_ENV=/tmp/dbus_env.sh
AUTO_RECOVER_CONFIG=/mnt/UDISK/sanyin-config/airplay-auto-recover
PAYLOAD='{"port":5002,"mac":"112233445577","device":"三音云音箱-C931"}'

log_message() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') $*" >> "$LOG_FILE"
}

is_ready() {
    [ -s "$DBUS_ENV" ] || return 1
    pidof SPlayer >/dev/null 2>&1 || return 1
    ifconfig wlan0 2>/dev/null | grep -q 'inet addr:'
}

is_listening() {
    netstat -lnt 2>/dev/null | grep -q ":${PORT} "
}

auto_recover_enabled() {
	# Missing config preserves the historical enabled behavior. Only the exact
	# value "disabled" turns the watchdog off.
	[ ! -f "$AUTO_RECOVER_CONFIG" ] || ! grep -qx 'disabled' "$AUTO_RECOVER_CONFIG"
}

start_airplay() {
    . "$DBUS_ENV" || return 1

    /usr/bin/dbus-send \
        --session \
        --type=method_call \
        --dest=netease.ihw.splayer \
        /netease/ihw/splayer \
        netease.ihw.SmartAudio.API \
        uint32:0 \
        uint32:2048 \
        uint32:7425 \
        uint32:66 \
        string:"$PAYLOAD"
}

log_message 'AirPlay restore watchdog started'

while :; do
	if auto_recover_enabled && is_ready && ! is_listening; then
        log_message 'AirPlay listener missing; sending native SPlayer start command'
        if start_airplay; then
            sleep 3
            if is_listening; then
                log_message 'AirPlay enabled on TCP port 5002'
            else
                log_message 'Start command sent, but TCP port 5002 is not listening yet'
            fi
        else
            log_message 'Failed to send SPlayer start command'
        fi
    fi

    sleep "$CHECK_INTERVAL"
done
