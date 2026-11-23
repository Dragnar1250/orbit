#!/bin/bash
# Smoke test on a real Mac (GitHub's macOS runner): start Orbit.app, drive it through its settings
# API, check the log, take screenshots (as small images in the log), quit it cleanly.
set -uo pipefail
APP=dist/Orbit.app
DIR="$HOME/Library/Application Support/OrbitLyrics"
rm -rf "$DIR"
"$APP/Contents/MacOS/Orbit" > /tmp/orbit-stdout.log 2>&1 &
PID=$!
fail() { echo "::error::$1"; echo "---- orbit.log ----"; cat "$DIR/orbit.log" 2>/dev/null; echo "---- crash.log ----"; cat "$DIR/crash.log" 2>/dev/null; echo "---- stdout ----"; cat /tmp/orbit-stdout.log; exit 1; }
alive() { kill -0 $PID 2>/dev/null || fail "Orbit exited during: $1"; }
shot() {
  screencapture -x "/tmp/$1.png" 2>/dev/null || { echo "screenshot $1 failed"; return; }
  sips -Z 512 -s format jpeg "/tmp/$1.png" --out "/tmp/$1.jpg" >/dev/null 2>&1
  base64 -i "/tmp/$1.jpg" | tr -d '\n' | fold -w 3000 | awk -v n="$1" '{printf "SHOT %s %03d %s\n", n, NR-1, $0}'
}
sleep 6
alive start
grep -q "Orbit .* starting" "$DIR/orbit.log" || fail "no start line in orbit.log"
TOK=$(cat "$DIR/settings.key")
BASE=http://127.0.0.1:47330
curl -sf "$BASE/api/state?t=$TOK" | head -c 200 >/dev/null || fail "settings API doesn't answer"
echo "settings API answers"
set_opt() { curl -sf -X POST "$BASE/api/set?t=$TOK" -H 'Content-Type: application/json' -d "{\"key\":\"$1\",\"value\":$2}" >/dev/null || fail "set $1"; }
act() { curl -sf -X POST "$BASE/api/action?t=$TOK" -H 'Content-Type: application/json' -d "{\"name\":\"$1\",\"value\":$2}" >/dev/null || fail "action $1"; }
shot 01-start
for s in $(seq 0 28); do set_opt LyricsStyle $s; done
for v in 20 40 80 120; do set_opt TextSize $v; sleep 0.2; done
set_opt ShowLyrics false; sleep 0.5; set_opt ShowLyrics true; sleep 0.5
alive "settings changes"
# Apple Music is on every Mac: start it so Orbit has to talk to it (no song plays here)
open -g -a Music 2>/dev/null; sleep 6; alive "Music running"
shot 02-music-open
osascript -e 'tell application "Music" to quit' 2>/dev/null; sleep 2; alive "Music closed"
echo "memory: $(ps -o rss= -p $PID | awk '{printf "%d MB", $1/1024}')"
# idle cost: CPU time used over 20 s of doing nothing
cpu() { ps -o time= -p $PID | awk -F'[:.]' '{ if (NF==4) print $1*60+$2+$3/100; else print $1*3600+$2*60+$3 }'; }
c0=$(cpu); sleep 20; c1=$(cpu)
echo "idle CPU: $(awk -v a=$c0 -v b=$c1 'BEGIN{printf "%.2f s in 20 s = %.2f%% of one core", b-a, (b-a)/20*100}')"
echo "memory after idling: $(ps -o rss= -p $PID | awk '{printf "%d MB", $1/1024}')"
act quit true
for i in $(seq 1 20); do kill -0 $PID 2>/dev/null || break; sleep 0.5; done
kill -0 $PID 2>/dev/null && fail "Orbit didn't quit within 10 s"
wait $PID; code=$?
[ $code -eq 0 ] || fail "Orbit quit with code $code"
grep -qE "panic:|fatal error:|goroutine [0-9]+" "$DIR/crash.log" 2>/dev/null && fail "crash"
echo "---- orbit.log ----"; cat "$DIR/orbit.log"
echo "Orbit (macOS) went through everything without crashing"
