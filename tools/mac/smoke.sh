#!/bin/bash
# Smoke test on a real Mac (GitHub's macOS runner): start Orbit.app, drive it through its settings
# API, check the log, take screenshots (as small images in the log), quit it cleanly.
set -uo pipefail
APP=dist/Orbit.app
DIR="$HOME/Library/Application Support/OrbitLyrics"
rm -rf "$DIR"
ORBIT_NO_SETTINGS=1 "$APP/Contents/MacOS/Orbit" > /tmp/orbit-stdout.log 2>&1 &
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
osascript -e 'tell application "Safari" to quit' 2>/dev/null; sleep 2 # the settings page opened on the first start: out of the way
for k in ShowLyrics ShowVinyl ShowHalo ShowClock ShowWeather ShowGlow; do set_opt $k true; done
sleep 3; alive "all widgets on"
shot 02-widgets
for s in $(seq 0 28); do set_opt LyricsStyle $s; sleep 0.1; done
for s in 0 1 2 3 4 5; do set_opt HaloStyle $s; sleep 0.3; done; alive "visualizer styles"
for s in 0 1 2 3; do set_opt DiscStyle $s; sleep 0.3; done; alive "turntable styles"
for s in 0 1 2 3 4 5; do set_opt ClockStyle $s; sleep 0.3; done; alive "clock styles"
for v in 20 40 80 120; do set_opt TextSize $v; sleep 0.2; done
osascript -e 'tell application "Safari" to quit' 2>/dev/null; sleep 1
act editMode true; sleep 2; alive "arrange mode"; shot 03-arrange
curl -sf -X POST "$BASE/api/action?t=$TOK" -H 'Content-Type: application/json' -d '{"name":"widget","value":{"name":"vinyl","x":30,"y":60,"scale":1.4}}' >/dev/null || fail "move widget"
sleep 1; alive "move widget"; act editMode false; sleep 1
shot 04-moved
set_opt ShowGlow false
# the settings in Orbit's own window (no browser)
act openSettings true; sleep 4; alive "settings window"
osascript -e 'tell application "System Events" to get name of every window of (first process whose unix id is '$PID')' 2>/dev/null | sed 's/^/windows: /' || true
shot 05-settings-window
# Apple Music is on every Mac: start it so Orbit has to talk to it (no song plays here)
open -g -a Music 2>/dev/null; sleep 6; alive "Music running"
shot 02-music-open
osascript -e 'tell application "Music" to quit' 2>/dev/null; sleep 2; alive "Music closed"
echo "memory: $(ps -o rss= -p $PID | awk '{printf "%d MB", $1/1024}')"
# idle cost: CPU time used over 20 s of doing nothing
cpu() { ps -o time= -p $PID | tr -d ' ' | awk -F: '{ if (NF==3) print $1*3600+$2*60+$3; else print $1*60+$2 }'; } # [h:]m:ss.hh
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

# ---- second run: a song "plays" (ORBIT_FAKE_PLAY) — visualizer, spinning record, lyrics, motion;
# the cost while music plays; and only one Orbit at a time
ORBIT_NO_SETTINGS=1 ORBIT_FAKE_PLAY=1 "$APP/Contents/MacOS/Orbit" > /tmp/orbit-stdout2.log 2>&1 &
PID=$!
sleep 8; alive "playing"
TOK=$(cat "$DIR/settings.key")
for k in ShowLyrics ShowVinyl ShowHalo ShowClock ShowWeather; do set_opt $k true; done
set_opt Motion 1; sleep 6; alive "playing with everything on"
shot 06-playing
for s in 0 1 2 3 4 5; do set_opt HaloStyle $s; sleep 1; alive "visualizer style $s while playing"; done
shot 07-visualizer
# full screen while a song plays: both kinds, every background, quick open/close
set_opt FsKind 0; act fullscreen true; sleep 5; alive "full screen (music display)"; shot 08-fs-music
for b in 0 1 2 3 4 5 6; do set_opt FsBg $b; sleep 0.8; alive "music display background $b"; done
act fullscreen false; sleep 1
set_opt FsKind 1; act fullscreen true; sleep 5; alive "full screen (desktop skin)"; shot 09-fs-skin
for m in 0 1 2; do set_opt SkinCentre $m; sleep 0.8; alive "skin middle $m"; done
for b in 0 1 2 3 4 5 6 7 8; do set_opt SkinBg $b; sleep 0.8; alive "skin background $b"; done
set_opt SkinBg 9; sleep 8; alive "music video background"; shot 10-fs-video
fc0=$(cpu); sleep 10; fc1=$(cpu)
echo "CPU in full screen: $(awk -v a=$fc0 -v b=$fc1 'BEGIN{printf "%.2f s in 10 s = %.2f%% of one core", b-a, (b-a)/10*100}')"
echo "memory in full screen: $(ps -o rss= -p $PID | awk '{printf "%d MB", $1/1024}')"
act fullscreen false; sleep 2; alive "leave full screen"
for i in 1 2 3 4 5 6; do act fullscreen true; sleep 0.4; act fullscreen false; sleep 0.4; done
sleep 2; alive "full screen opened and closed 6 times"
grep -E "full screen|video" "$DIR/orbit.log" | tail -6
echo "memory while playing: $(ps -o rss= -p $PID | awk '{printf "%d MB", $1/1024}')"
# where the time goes while playing (macOS's sampler: native + Go stacks)
sample $PID 5 -mayDie -file /tmp/orbit-sample.txt >/dev/null 2>&1 && awk '/Sort by top of stack/,0' /tmp/orbit-sample.txt | head -45 | sed 's/^/PROFILE /' 
c0=$(cpu); sleep 20; c1=$(cpu)
echo "CPU while playing: $(awk -v a=$c0 -v b=$c1 'BEGIN{printf "%.2f s in 20 s = %.2f%% of one core", b-a, (b-a)/20*100}')"
echo "memory after 20 s playing: $(ps -o rss= -p $PID | awk '{printf "%d MB", $1/1024}')"
grep -E "screen [0-9]+x" "$DIR/orbit.log" | tail -1
# a second copy takes over and the first one leaves
OLD=$PID
ORBIT_NO_SETTINGS=1 ORBIT_FAKE_PLAY=1 "$APP/Contents/MacOS/Orbit" > /tmp/orbit-stdout3.log 2>&1 &
PID=$!
sleep 6
kill -0 $OLD 2>/dev/null && fail "the older Orbit is still running next to the new one"
alive "the new copy after taking over"
echo "only one Orbit runs: $(pgrep -x Orbit | wc -l | tr -d ' ')"
tail -5 "$DIR/orbit.log"
PORT=$(grep -oE "settings window on 127.0.0.1:[0-9]+" "$DIR/orbit.log" | tail -1 | cut -d: -f2)
BASE=http://127.0.0.1:${PORT:-47330}
TOK=$(cat "$DIR/settings.key")
act quit true; sleep 3
grep -E "closed [0-9]+ other" "$DIR/orbit.log" | tail -1
echo "Orbit (macOS) played without crashing"
