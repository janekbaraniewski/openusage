#!/usr/bin/env bash
# OpenUsage SketchyBar provider switcher. Pinning is delegated to the daemon.

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
OPENUSAGE_BIN="${OPENUSAGE_BIN:-openusage}"
CACHE_DIR="${OPENUSAGE_SKETCHYBAR_CACHE_DIR:-$HOME/.cache/openusage/sketchybar}"
LIST_CACHE="$CACHE_DIR/list.json"
SWITCHER="${NAME:-ai_switcher}"
TRIGGER="${OPENUSAGE_SKETCHYBAR_SWITCHER_TRIGGER:-click}"

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

if [ -z "${1:-}" ]; then
  case "$TRIGGER" in
    hover)
      case "${SENDER:-}" in
        mouse.entered)
          close_popups
          ;;
        # Only the bar-scoped exit closes the picker. mouse.exited fires when
        # the pointer leaves this item, which includes moving down into this
        # very popup, so acting on it would dismiss the menu mid-reach.
        mouse.exited.global)
          close_popups
          exit 0
          ;;
        *)
          exit 0
          ;;
      esac
      ;;
    *)
      if [ "${SENDER:-}" != "mouse.clicked" ]; then
        exit 0
      fi
      popup_state=$(sketchybar --query "$SWITCHER" 2>/dev/null | jq -r '.popup.drawing // "off"')
      if [ "$popup_state" = "on" ]; then
        close_popups
        exit 0
      fi
      close_popups
      ;;
  esac
fi

if [ "${1:-}" = "--auto" ]; then
  "$OPENUSAGE_BIN" active unpin >/dev/null 2>&1 || exit 1
elif [ -n "${1:-}" ]; then
  "$OPENUSAGE_BIN" active pin "$1" >/dev/null 2>&1 || exit 1
fi

if [ -n "${1:-}" ]; then
  NAME=ai SENDER=forced "$SCRIPT_DIR/ai-usage.sh" >/dev/null 2>&1
  # Close by explicit item name, not via $SWITCHER: this path runs as a row's
  # click_script, so NAME is the clicked row rather than the picker, and a
  # --set against it would silently leave the menu open.
  close_popups
  exit 0
fi

mkdir -p "$CACHE_DIR" 2>/dev/null
payload=""
if payload=$("$OPENUSAGE_BIN" active list --json 2>/dev/null) &&
   printf '%s\n' "$payload" | jq -e 'type == "object" and (.candidates | type == "array")' >/dev/null 2>&1; then
  tmp=$(mktemp "$CACHE_DIR/list.XXXXXX" 2>/dev/null) || tmp=""
  if [ -n "$tmp" ]; then
    printf '%s\n' "$payload" >"$tmp" && mv -f "$tmp" "$LIST_CACHE"
  fi
fi
if [ -z "$payload" ] && [ -f "$LIST_CACHE" ]; then
  payload=$(<"$LIST_CACHE")
fi

sketchybar --remove '/openusage.switch.row\..*/' >/dev/null 2>&1
select_path="$SCRIPT_DIR/provider-select.sh"
printf -v select_path '%q' "$select_path"

if [ -z "$payload" ] || ! printf '%s\n' "$payload" | jq -e 'type == "object"' >/dev/null 2>&1; then
  item="openusage.switch.row.0"
  sketchybar --add item "$item" "popup.$SWITCHER" \
    --set "$item" icon="!" label="no usage data" icon.color="${OPENUSAGE_SKETCHYBAR_WARN_COLOR:-0xffeed49f}" \
      label.color="${OPENUSAGE_SKETCHYBAR_WARN_COLOR:-0xffeed49f}" click_script=""
else
  selected=$(printf '%s\n' "$payload" | jq -r '.selected // ""')
  pinned=$(printf '%s\n' "$payload" | jq -r '.pinned // ""')
  auto_selected=0
  [ -z "$pinned" ] && auto_selected=1
  rows=$(printf '%s\n' "$payload" | jq -r --arg selected "$selected" --arg pinned "$pinned" --argjson auto_selected "$auto_selected" '
    .candidates as $candidates |
    ($candidates
      | map(.provider_id // "")
      | group_by(.)
      | map({key: .[0], value: length})
      | from_entries) as $provider_counts |
    [["--auto", "Auto", (if $auto_selected == 1 then "1" else "0" end), "0"]] +
    [ $candidates[] |
      (.provider_id // "") as $provider |
      (.display // $provider) as $display |
      (.account_id // "default") as $account |
      (.severity // "unknown") as $severity |
      ($provider_counts[$provider] // 1) as $provider_count |
      (if $provider_count < 2 or $account == "default" or $account == $provider then
         $display
       elif ($account | startswith($provider + "-")) then
         ($account | split("-") | .[1:] | join("-")) as $suffix |
         (if $suffix == "" or $suffix == "default" then $display else ($display + " · " + $suffix) end)
       else
         ($display + " · " + $account)
       end) as $label |
      [
        .key,
        $label,
        (if .key == $selected then "1" else "0" end),
        (if .key == $pinned then "1" else "0" end),
        $severity
      ]
    ] | .[] | @tsv
  ')
  index=0
  while IFS=$'\t' read -r key label selected_row pinned_row severity; do
    [ -z "$key" ] && continue
    if [ "$key" = "--auto" ]; then
      click_script="$select_path --auto"
      icon="↻"
      indicator_color="${OPENUSAGE_SKETCHYBAR_TEXT_COLOR:-0xffcad3f5}"
    else
      printf -v quoted_key '%q' "$key"
      click_script="$select_path $quoted_key"
      icon="●"
      case "$severity" in
        good) indicator_color="${OPENUSAGE_SKETCHYBAR_GOOD_COLOR:-0xffa6da95}" ;;
        warn) indicator_color="${OPENUSAGE_SKETCHYBAR_WARN_COLOR:-0xffeed49f}" ;;
        bad) indicator_color="${OPENUSAGE_SKETCHYBAR_BAD_COLOR:-0xffed8796}" ;;
        *) indicator_color="${OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR:-0xffcad3f5}" ;;
      esac
    fi
    color="${OPENUSAGE_SKETCHYBAR_TEXT_COLOR:-0xffcad3f5}"
    [ "$selected_row" = "1" ] && color="${OPENUSAGE_SKETCHYBAR_GOOD_COLOR:-0xffa6da95}"
    [ "$pinned_row" = "1" ] && label="🔒 $label"
    item="openusage.switch.row.$index"
    sketchybar --add item "$item" "popup.$SWITCHER" \
      --set "$item" icon="$icon" icon.font="Hack Nerd Font:Regular:15.0" icon.color="$indicator_color" icon.width=16 icon.padding_right=4 \
        label="$label" label.font="Hack Nerd Font:Regular:12.0" label.color="$color" \
        click_script="$click_script" background.padding_left=6 background.padding_right=6
    index=$((index + 1))
  done <<< "$rows"
fi

sketchybar --set "$SWITCHER" popup.horizontal=on popup.drawing=on
