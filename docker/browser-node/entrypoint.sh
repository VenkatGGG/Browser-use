#!/usr/bin/env bash
set -euo pipefail

DISPLAY_NUM="${DISPLAY_NUM:-99}"
SCREEN_GEOMETRY="${XVFB_SCREEN_GEOMETRY:-1280x720x24}"
CHROME_PORT="${CHROME_DEBUG_PORT:-9222}"
BROWSER_BIN="${BROWSER_BIN:-/usr/local/bin/browser-bin}"

export DISPLAY=":${DISPLAY_NUM}"

Xvfb "${DISPLAY}" -screen 0 "${SCREEN_GEOMETRY}" -nolisten tcp > /tmp/xvfb.log 2>&1 &
XVFB_PID=$!

"${BROWSER_BIN}" \
  --headless=new \
  --disable-gpu \
  --no-sandbox \
  --remote-debugging-address=0.0.0.0 \
  --remote-debugging-port="${CHROME_PORT}" \
  --user-data-dir=/tmp/chrome \
  --window-size=1280,720 \
  --no-first-run \
  --no-default-browser-check \
  about:blank > /tmp/chrome.log 2>&1 &
CHROME_PID=$!

cleanup() {
  kill "${CHROME_PID}" "${XVFB_PID}" >/dev/null 2>&1 || true
}

trap cleanup EXIT INT TERM

exec /usr/local/bin/node-agent
