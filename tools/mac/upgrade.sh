#!/bin/bash
# The way people really get a new Orbit: preview 6 (1.18) has been running with its settings and
# caches, the Mac is in Turkish, and the new Orbit.app is opened from Finder (LaunchServices),
# sometimes while the old one is still in the menu bar. It must start every time.
set -uo pipefail
DIR="$HOME/Library/Application Support/OrbitLyrics"
OLD=/tmp/orbit-old
fail() { echo "::error::$1"; echo "---- orbit.log ----"; tail -n 80 "$DIR/orbit.log" 2>/dev/null; echo "---- crash.log ----"; tail -n 120 "$DIR/crash.log" 2>/dev/null; echo "---- system log ----"; log show --last 2m --predicate 'process == "Orbit" OR eventMessage CONTAINS "Orbit"' 2>/dev/null | tail -n 60; exit 1; }
git fetch -q --depth=1 origin 298c5f5ac || git fetch -q origin
git worktree add -f "$OLD" 298c5f5 >/dev/null 2>&1 || fail "no preview 6 source"
(cd "$OLD" && CGO_ENABLED=1 go build -o /tmp/orbit-old-bin .) || fail "preview 6 did not build"
rm -rf "$DIR"
ORBIT_NO_SETTINGS=1 ORBIT_FAKE_PLAY=1 /tmp/orbit-old-bin >/dev/null 2>&1 &
OPID=$!
sleep 8
TOK=$(cat "$DIR/settings.key")
for k in ShowLyrics ShowVinyl ShowHalo ShowClock ShowWeather ShowGlow; do
  curl -sf -X POST "http://127.0.0.1:47330/api/set?t=$TOK" -H 'Content-Type: application/json' -d "{\"key\":\"$k\",\"value\":true}" >/dev/null
done
sleep 4
echo "preview 6 ran; its files:"; ls "$DIR" "$DIR/cache" 2>/dev/null | head -30
defaults write -g AppleLanguages -array tr-TR en-US
defaults write -g AppleLocale tr_TR
rm -rf /Applications/Orbit.app && cp -R dist/Orbit.app /Applications/
xattr -w com.apple.quarantine "0081;$(printf %x $(date +%s));Safari;" /Applications/Orbit.app 2>/dev/null || true
xattr -dr com.apple.quarantine /Applications/Orbit.app # (what "Open Anyway" does)
check() { # $1 = what
  for i in $(seq 1 20); do pgrep -f /Applications/Orbit.app/Contents/MacOS/Orbit >/dev/null && break; sleep 0.5; done
  sleep 8
  pgrep -f /Applications/Orbit.app/Contents/MacOS/Orbit >/dev/null || fail "Orbit.app did not stay open: $1"
  grep -E "starting|PANIC|panic" "$DIR/orbit.log" | tail -n 3
  echo "ok: $1"
}
open /Applications/Orbit.app || fail "open: preview 6 still running"
check "opened from Finder while preview 6 was running, Mac in Turkish"
kill -0 $OPID 2>/dev/null && echo "(preview 6 still running?)"
pkill -f /Applications/Orbit.app/Contents/MacOS/Orbit; sleep 2
open /Applications/Orbit.app || fail "open: second start"
check "second start (its own settings now)"
pkill -f /Applications/Orbit.app/Contents/MacOS/Orbit; sleep 1
kill $OPID 2>/dev/null
defaults delete -g AppleLanguages; defaults delete -g AppleLocale
echo "upgrade from preview 6: fine"
