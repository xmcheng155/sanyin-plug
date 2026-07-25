#!/bin/sh

# 仅供受控诊断与适配层验证使用：参数必须是固件定义的 0..6。
mode="${1:-}"
case "$mode" in
  0|1|2|3|4|5|6) ;;
  *) printf 'result=invalid_mode\n'; exit 0 ;;
esac

if [ ! -s /tmp/dbus_env.sh ] || ! pidof netease_control_center >/dev/null 2>&1; then
  printf 'result=not_ready\n'
  exit 0
fi

. /tmp/dbus_env.sh || { printf 'result=not_ready\n'; exit 0; }
capture="/tmp/sanyin-eq-event.$$"
dbus-monitor --session "type='signal',interface='netease.ihw.SmartAudio',member='Notify'" > "$capture" 2>/dev/null &
monitor_pid=$!
sleep 1

http_code="$(curl --max-time 3 --silent --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:1705/eq/$mode" 2>/dev/null || true)"
if [ "$http_code" != 200 ]; then
  kill "$monitor_pid" 2>/dev/null || true
  rm -f "$capture"
  printf 'result=http_failed\n'
  exit 0
fi

attempt=0
result=verification_timeout
while [ "$attempt" -lt 8 ]; do
  if grep -q 'uint32 2570' "$capture" && grep -Eq "eqType[^0-9]*${mode}([^0-9]|$)" "$capture"; then
    result=selected_confirmed
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done

kill "$monitor_pid" 2>/dev/null || true
rm -f "$capture"
if [ "$result" = selected_confirmed ]; then
  config_dir=/mnt/UDISK/sanyin-config
  mkdir -p "$config_dir" || result=confirmed_no_persist
  if [ "$result" = selected_confirmed ]; then
    printf '%s\n' "$mode" > "$config_dir/.eq-last-confirmed.tmp" && mv "$config_dir/.eq-last-confirmed.tmp" "$config_dir/eq-last-confirmed" || result=confirmed_no_persist
  fi
fi
printf 'result=%s\n' "$result"
