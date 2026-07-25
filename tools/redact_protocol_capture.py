#!/usr/bin/env python3
"""Redact device identifiers and network data from a protocol capture."""

from __future__ import annotations

import argparse
import re
from pathlib import Path


MAC = re.compile(r"(?i)(?<![0-9a-f])(?:[0-9a-f]{2}:){5}[0-9a-f]{2}(?![0-9a-f])")
IPV4 = re.compile(r"(?<!\d)(?:\d{1,3}\.){3}\d{1,3}(?!\d)")
UUID = re.compile(
    r"(?i)(?<![0-9a-f])[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-"
    r"[0-9a-f]{4}-[0-9a-f]{12}(?![0-9a-f])"
)
LONG_HEX = re.compile(r"(?i)(?<![0-9a-f])[0-9a-f]{16,}(?![0-9a-f])")
URL = re.compile(r"https?://[^\s\"\\]+")
ESSID = re.compile(r'(?i)(ESSID:)["\'][^"\']*["\']')
ESSID_LOG = re.compile(r"(?i)(\bessidStr\s+is\s+)[^\r\n]*")
SENSITIVE_FIELD = re.compile(
    r"(?i)("
    r"(?:\\?[\"'])?"
    r"(?:wifiSSID|wifi_ssid|ssid|bssid|btAddr|wifi_mac|mac|uuid|deviceid|"
    r"chipId|token|password|passwd|pwd|psk|sharedkey|sessionId|responseid|cloudId)"
    r"(?:\\?[\"'])?\s*[:=]\s*"
    r")"
    r"(?:\\?[\"'][^\"'\r\n]*\\?[\"']|[^,}\s\r\n]+)"
)


def redact(text: str) -> str:
    text = URL.sub("[REDACTED_URL]", text)
    text = ESSID.sub(r'\1"[REDACTED_SSID]"', text)
    text = ESSID_LOG.sub(r"\1[REDACTED_SSID]", text)
    text = SENSITIVE_FIELD.sub(r'\1"[REDACTED]"', text)
    text = MAC.sub("[REDACTED_MAC]", text)
    text = IPV4.sub("[REDACTED_IP]", text)
    text = UUID.sub("[REDACTED_UUID]", text)
    return LONG_HEX.sub("[REDACTED_ID]", text)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path)
    parser.add_argument("destination", type=Path)
    args = parser.parse_args()

    source_text = args.source.read_text(encoding="utf-8", errors="replace")
    args.destination.write_text(redact(source_text), encoding="utf-8")


if __name__ == "__main__":
    main()
