#!/bin/sh

set -u

config_dir=/mnt/UDISK/sanyin-config
wifi_dir=/mnt/UDISK/wifi
live_config=$wifi_dir/wpa_supplicant.conf
pending_config=$config_dir/wifi-pending.conf
pending_ssid=$config_dir/wifi-pending-ssid
rollback_config=$config_dir/wifi-rollback.conf
rollback_ssid=$config_dir/wifi-rollback-ssid
marker=$config_dir/wifi-switch-pending
last_result=$config_dir/wifi-last-result
control_dir=$wifi_dir/sockets

write_result() {
  value="$1"
  mkdir -p "$config_dir" || return 1
  printf '%s\n' "$value" > "$config_dir/.wifi-last-result.tmp" &&
    chmod 0600 "$config_dir/.wifi-last-result.tmp" &&
    mv "$config_dir/.wifi-last-result.tmp" "$last_result"
}

reconfigure() {
  wpa_cli -p "$control_dir" -i wlan0 reconfigure 2>/dev/null | grep -qx OK
}

connection_ready() {
  expected="$1"
  status="$(wpa_cli -p "$control_dir" -i wlan0 status 2>/dev/null)"
  state="$(printf '%s\n' "$status" | sed -n 's/^wpa_state=//p' | head -n 1)"
  current="$(printf '%s\n' "$status" | sed -n 's/^ssid=//p' | head -n 1)"
  [ "$state" = COMPLETED ] || return 1
  [ -z "$expected" ] || [ "$current" = "$expected" ] || return 1
  ifconfig wlan0 2>/dev/null | grep -q 'inet addr:' || return 1
  gateway="$(route -n 2>/dev/null | awk '$1 == "0.0.0.0" && $8 == "wlan0" { print $2; exit }')"
  [ -n "$gateway" ] || return 1
  ping -c 1 -W 2 "$gateway" >/dev/null 2>&1
}

wait_for_connection() {
  expected="$1"
  attempts="${2:-45}"
  current_attempt=0
  while [ "$current_attempt" -lt "$attempts" ]; do
    connection_ready "$expected" && return 0
    current_attempt=$((current_attempt + 1))
    sleep 1
  done
  return 1
}

cleanup_transaction() {
  rm -f "$marker" "$rollback_config" "$rollback_ssid" "$pending_config" "$pending_ssid"
}

rollback() {
  trap - EXIT HUP INT TERM
  if [ ! -s "$rollback_config" ]; then
    write_result rollback_failed || true
    rm -f "$marker" "$pending_config" "$pending_ssid"
    return 1
  fi
  cp "$rollback_config" "$wifi_dir/.wpa_supplicant.rollback" &&
    chmod 0600 "$wifi_dir/.wpa_supplicant.rollback" &&
    mv "$wifi_dir/.wpa_supplicant.rollback" "$live_config" || {
      write_result rollback_failed || true
      return 1
    }
  sync
  reconfigure || true
  original_ssid=""
  [ -f "$rollback_ssid" ] && original_ssid="$(cat "$rollback_ssid")"
  if wait_for_connection "$original_ssid" 45; then
    write_result rolled_back || true
    cleanup_transaction
    return 0
  fi
  write_result rollback_failed || true
  rm -f "$marker" "$rollback_ssid" "$pending_config" "$pending_ssid"
  return 1
}

recover() {
  if [ -f "$marker" ]; then
    if rollback; then
      write_result recovered_after_restart || true
      printf 'result=recovered_after_restart\n'
    else
      printf 'result=rollback_failed\n'
    fi
  else
    printf 'result=no_pending_transaction\n'
  fi
}

switch_network() {
  [ -s "$pending_config" ] && [ -s "$pending_ssid" ] && [ -s "$live_config" ] || {
    printf 'result=invalid_pending_config\n'
    return 0
  }
  expected_ssid="$(cat "$pending_ssid")"
  [ -n "$expected_ssid" ] || { printf 'result=invalid_pending_config\n'; return 0; }
  mkdir -p "$config_dir" || { printf 'result=write_failed\n'; return 0; }
  cp "$live_config" "$rollback_config" && chmod 0600 "$rollback_config" || {
    printf 'result=backup_failed\n'
    return 0
  }
  original_ssid="$(wpa_cli -p "$control_dir" -i wlan0 status 2>/dev/null | sed -n 's/^ssid=//p' | head -n 1)"
  printf '%s\n' "$original_ssid" > "$rollback_ssid" && chmod 0600 "$rollback_ssid" || {
    rm -f "$rollback_config" "$rollback_ssid"
    printf 'result=backup_failed\n'
    return 0
  }
  printf 'switching\n' > "$marker" && chmod 0600 "$marker" || {
    rm -f "$rollback_config"
    printf 'result=write_failed\n'
    return 0
  }
  trap 'rollback >/dev/null 2>&1 || true' EXIT
  trap 'rollback >/dev/null 2>&1 || true; exit 1' HUP INT TERM
  cp "$pending_config" "$wifi_dir/.wpa_supplicant.pending" &&
    chmod 0600 "$wifi_dir/.wpa_supplicant.pending" &&
    mv "$wifi_dir/.wpa_supplicant.pending" "$live_config" || {
      rollback || true
      printf 'result=rolled_back\n'
      return 0
    }
  sync
  if reconfigure && wait_for_connection "$expected_ssid" 45; then
    trap - EXIT HUP INT TERM
    write_result succeeded || true
    cleanup_transaction
    printf 'result=succeeded\n'
    return 0
  fi
  if rollback; then
    printf 'result=rolled_back\n'
  else
    printf 'result=rollback_failed\n'
  fi
}

case "${1:-}" in
  switch) switch_network ;;
  recover) recover ;;
  *) printf 'result=invalid_action\n' ;;
esac
