#!/usr/bin/env bash
# OpenUsage SketchyBar bar item. Keep this script boring: the CLI owns
# provider selection and narration; this file only paints the result.

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
OPENUSAGE_BIN="${OPENUSAGE_BIN:-openusage}"
CACHE_DIR="${OPENUSAGE_SKETCHYBAR_CACHE_DIR:-$HOME/.cache/openusage/sketchybar}"
ACTIVE_CACHE="$CACHE_DIR/active.json"
STALE_AFTER="${OPENUSAGE_SKETCHYBAR_STALE_AFTER:-600}"
TRIGGER="${OPENUSAGE_SKETCHYBAR_USAGE_TRIGGER:-click}"

file_mtime() {
  stat -f %m "$1" 2>/dev/null || stat -c %Y "$1" 2>/dev/null || printf '0\n'
}

# Loose: the payload is shaped like an active response. Enough to paint from,
# whatever its status. Used only when the CLI could not be reached at all --
# a stale reading beats "AI unavailable".
parsable_active() {
  jq -e 'type == "object" and (.status | type == "string")' >/dev/null 2>&1
}

# Strict: the payload actually carries a quota reading. Only these are worth
# caching, and only these may displace a cached quota-bearing snapshot.
quota_bearing() {
  jq -e 'type == "object" and .status == "ok" and (.label // "") != "quota unavailable"' >/dev/null 2>&1
}

# Event-backed selection is still authoritative when its provider has no
# quota reading. Do not let an older quota snapshot from another provider win
# merely because the current provider's quota label is unavailable.
event_backed() {
  jq -e 'type == "object" and (.source == "events" or .source == "pinned")' >/dev/null 2>&1
}

# The active CLI has a bounded timeout and may return a local recency answer
# just as the daemon finishes its read-model query. Retry once so the bar does
# not mistake that timing race for authoritative local state.
fetch_fresh_active() {
  local candidate="" local_fallback="" attempt
  for attempt in 1 2; do
    candidate="$("$OPENUSAGE_BIN" active --json 2>/dev/null)" || candidate=""
    if printf '%s\n' "$candidate" | parsable_active; then
      if printf '%s\n' "$candidate" | quota_bearing ||
         printf '%s\n' "$candidate" | event_backed; then
        printf '%s\n' "$candidate"
        return 0
      fi
      local_fallback="$candidate"
    fi
    [ "$attempt" -lt 2 ] && sleep 0.25
  done
  [ -n "$local_fallback" ] && printf '%s\n' "$local_fallback"
  [ -n "$local_fallback" ]
}

close_popups() {
  local helper="$HOME/.config/sketchybar/plugins/popup_close.sh"
  [ -n "$CONFIG_DIR" ] && helper="$CONFIG_DIR/plugins/popup_close.sh"
  [ -n "$OPENUSAGE_SKETCHYBAR_CLOSE_SCRIPT" ] && helper="$OPENUSAGE_SKETCHYBAR_CLOSE_SCRIPT"
  if [ -x "$helper" ]; then
    "$helper" >/dev/null 2>&1
  else
    sketchybar --set ai popup.drawing=off >/dev/null 2>&1
    sketchybar --set ai_switcher popup.drawing=off >/dev/null 2>&1
  fi
}

open_popup() {
  if [ "${NAME:-ai}" != "ai" ]; then
    exit 0
  fi
  close_popups
  exec "$SCRIPT_DIR/usage-popup.sh"
}

paint() {
  local text="$1"
  local color="$2"
  sketchybar --set "${NAME:-ai}" drawing=on \
    icon="${OPENUSAGE_SKETCHYBAR_ICON:-󰚩}" icon.color="$color" \
    label="$text" label.color="$color"
}

case "$TRIGGER" in
  hover)
    case "${SENDER:-}" in
      mouse.entered)
        open_popup
        ;;
      mouse.exited|mouse.exited.global)
        close_popups
        exit 0
        ;;
    esac
    ;;
  *)
    case "${SENDER:-}" in
      mouse.clicked)
        # Never let a neighbouring item open this popup if SketchyBar reports
        # a surprising event target.
        if [ "${NAME:-ai}" != "ai" ]; then
          exit 0
        fi
        popup_state=$(sketchybar --query ai 2>/dev/null | jq -r '.popup.drawing // "off"')
        if [ "$popup_state" = "on" ]; then
          close_popups
          exit 0
        fi
        open_popup
        ;;
    esac
    ;;
esac

mkdir -p "$CACHE_DIR" 2>/dev/null
payload=""
fresh_payload=""
if fresh_payload=$(fetch_fresh_active); then
  # Local recency fallback can be valid JSON but have no quota. Do not let
  # that degraded answer overwrite the last live, quota-bearing snapshot. A
  # live event-backed answer remains authoritative even without quota data.
  if printf '%s\n' "$fresh_payload" | quota_bearing ||
     printf '%s\n' "$fresh_payload" | event_backed; then
    payload="$fresh_payload"
    tmp=$(mktemp "$CACHE_DIR/active.XXXXXX" 2>/dev/null) || tmp=""
    if [ -n "$tmp" ]; then
      printf '%s\n' "$payload" >"$tmp" && mv -f "$tmp" "$ACTIVE_CACHE"
    fi
  elif [ -f "$ACTIVE_CACHE" ] && quota_bearing <"$ACTIVE_CACHE"; then
    payload=$(<"$ACTIVE_CACHE")
  else
    payload="$fresh_payload"
  fi
fi

# The CLI could not be reached. Any parsable cached snapshot beats painting
# "AI unavailable", so this fallback stays deliberately loose.
if [ -z "$payload" ] && [ -f "$ACTIVE_CACHE" ] && parsable_active <"$ACTIVE_CACHE"; then
  payload=$(<"$ACTIVE_CACHE")
fi

if [ -z "$payload" ] || ! printf '%s\n' "$payload" | jq -e 'type == "object"' >/dev/null 2>&1; then
  paint "AI unavailable" "${OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR:-0xffcad3f5}"
  exit 0
fi

status=$(printf '%s\n' "$payload" | jq -r '.status // "unavailable"')
if [ "$status" != "ok" ]; then
  case "$status" in
    no_data) paint "AI no data" "${OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR:-0xffcad3f5}" ;;
    *)       paint "AI $status" "${OPENUSAGE_SKETCHYBAR_WARN_COLOR:-0xffeed49f}" ;;
  esac
  exit 0
fi

display=$(printf '%s\n' "$payload" | jq -r '.display // "AI"')
label=$(printf '%s\n' "$payload" | jq -r '.label // "quota unavailable"')
severity=$(printf '%s\n' "$payload" | jq -r '.severity // "unknown"')
color="${OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR:-0xffcad3f5}"
case "$severity" in
  good) color="${OPENUSAGE_SKETCHYBAR_GOOD_COLOR:-0xffa6da95}" ;;
  warn) color="${OPENUSAGE_SKETCHYBAR_WARN_COLOR:-0xffeed49f}" ;;
  bad)  color="${OPENUSAGE_SKETCHYBAR_BAD_COLOR:-0xffed8796}" ;;
esac

if [ -f "$ACTIVE_CACHE" ]; then
  now=$(date +%s)
  age=$((now - $(file_mtime "$ACTIVE_CACHE")))
  if [ "$age" -gt "$STALE_AFTER" ]; then
    label="$label · stale"
    color="${OPENUSAGE_SKETCHYBAR_WARN_COLOR:-0xffeed49f}"
  fi
fi

paint "$display $label" "$color"
