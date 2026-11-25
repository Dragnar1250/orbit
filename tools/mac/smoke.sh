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
for s in $(seq 0 30); do set_opt LyricsStyle $s; sleep 0.1; done
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
osascript -e 'tell application "Music" to quit' 2>/dev/null; sleep 2; killall Music 2>/dev/null; sleep 1; alive "Music closed"
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
ORBIT_PERF=1 ORBIT_NO_SETTINGS=1 ORBIT_FAKE_PLAY=1 "$APP/Contents/MacOS/Orbit" > /tmp/orbit-stdout2.log 2>&1 &
PID=$!
sleep 8; alive "playing"
TOK=$(cat "$DIR/settings.key")
for k in ShowLyrics ShowVinyl ShowHalo ShowClock ShowWeather; do set_opt $k true; done
set_opt Motion 1; sleep 6; alive "playing with everything on"
shot 06-playing
for s in 0 1 2 3 4 5; do set_opt HaloStyle $s; sleep 1; alive "visualizer style $s while playing"; done
shot 07-visualizer
for e in 0 1 2 3 4 5 6 7 8 9 10 11; do set_opt Effect $e; sleep 0.6; alive "lyrics effect $e"; done
set_opt Effect 2; sleep 2; shot 07b-strong-glow
set_opt Effect 1
# full screen (Cinema) while a song plays: covers, backgrounds, stages, a language
act fullscreen true; sleep 5; alive "Cinema"; shot 08-cinema
for c in 0 2; do for b in 0 1 2 3 4; do set_opt FsCover $c; set_opt FsBg $b; sleep 0.8; alive "Cinema cover $c background $b"; done; done
shot 09-cinema-record
for st in 0 1 12 28 30 31; do set_opt FsStage $st; sleep 0.8; alive "Cinema stage $st"; done
set_opt Lang '"tr"'; sleep 2; alive "Turkish"; shot 10-cinema-tr; set_opt Lang '""'
set_opt FsStage 28; set_opt FsCover 2; set_opt FsBg 1
fc0=$(cpu); sleep 10; fc1=$(cpu)
grep "perf:" "$DIR/orbit.log" | tail -1
echo "CPU in full screen: $(awk -v a=$fc0 -v b=$fc1 'BEGIN{printf "%.2f s in 10 s = %.2f%% of one core", b-a, (b-a)/10*100}')"
echo "memory in full screen: $(ps -o rss= -p $PID | awk '{printf "%d MB", $1/1024}')"
act fullscreen false; sleep 2; alive "leave full screen"
for i in 1 2 3 4 5 6; do act fullscreen true; sleep 0.4; act fullscreen false; sleep 0.4; done
sleep 2; alive "full screen opened and closed 6 times"
grep -E "full screen|video" "$DIR/orbit.log" | tail -6
echo "memory while playing: $(ps -o rss= -p $PID | awk '{printf "%d MB", $1/1024}')"
# where the time goes while playing (macOS's sampler: native + Go stacks)
# the cost while music plays, at each frame rate, with and without the screen glow
measure() {
  sleep 6; c0=$(cpu); sleep 15; c1=$(cpu)
  echo "CPU while playing ($1): $(awk -v a=$c0 -v b=$c1 'BEGIN{printf "%.2f s in 15 s = %.2f%% of one core", b-a, (b-a)/15*100}') · memory $(ps -o rss= -p $PID | awk '{printf "%d MB", $1/1024}')"
  grep "perf:" "$DIR/orbit.log" | tail -1
}
set_opt FPS 60; measure "60 fps, all widgets"
set_opt ShowGlow true; measure "60 fps, all widgets + screen glow"
set_opt FPS 30; measure "30 fps, all widgets + screen glow"
set_opt ShowGlow false; set_opt FPS 60
for k in ShowLyrics ShowVinyl ShowHalo ShowClock ShowWeather; do set_opt $k false; done
for k in ShowVinyl ShowHalo ShowLyrics; do set_opt $k true; measure "60 fps, only $k"; set_opt $k false; done
for k in ShowLyrics ShowVinyl ShowHalo ShowClock ShowWeather; do set_opt $k true; done
echo "memory after playing: $(ps -o rss= -p $PID | awk '{printf "%d MB", $1/1024}')"
grep -E "audio:|desktop widgets" "$DIR/orbit.log" | tail -3
grep -E "screen [0-9]+x" "$DIR/orbit.log" | tail -1
# a second copy takes over and the first one leaves — this one draws as many pixels as a Retina
# screen (ORBIT_SCALE=2), to see what a MacBook pays
OLD=$PID
ORBIT_SCALE=2 ORBIT_PERF=2 ORBIT_NO_SETTINGS=1 ORBIT_FAKE_PLAY=1 "$APP/Contents/MacOS/Orbit" > /tmp/orbit-stdout3.log 2>&1 &
PID=$!
sleep 6
kill -0 $OLD 2>/dev/null && fail "the older Orbit is still running next to the new one"
alive "the new copy after taking over"
echo "only one Orbit runs: $(pgrep -x Orbit | wc -l | tr -d ' ')"
tail -5 "$DIR/orbit.log"
PORT=$(grep -oE "settings window on 127.0.0.1:[0-9]+" "$DIR/orbit.log" | tail -1 | cut -d: -f2)
BASE=http://127.0.0.1:${PORT:-47330}
TOK=$(cat "$DIR/settings.key")
for k in ShowLyrics ShowVinyl ShowHalo ShowClock ShowWeather; do set_opt $k true; done
pprof_top() { sleep 13; [ -f "$1" ] && go tool pprof -top -cum -nodecount=60 "$1" 2>/dev/null | sed "s/^/PPROF $2 /"; }
set_opt FPS 60; kill -USR1 $PID; measure "Retina pixels, 60 fps, all widgets"
pprof_top /tmp/orbit-cpu-1.prof widgets
set_opt ShowGlow true; measure "Retina pixels, 60 fps, all widgets + screen glow"
set_opt ShowGlow false
for s in 0 3 6 10 14 20 26; do set_opt LyricsStyle $s; sleep 1.5; alive "lyrics style $s at Retina size"; done
for e in 0 1 2 3 4 5 6 7 8 9 10 11; do set_opt Effect $e; sleep 1; alive "lyrics effect $e at Retina size"; done
set_opt Effect 1; act fullscreen true; sleep 4; kill -USR1 $PID; sleep 6; alive "full screen at Retina size"
grep "perf:" "$DIR/orbit.log" | tail -1
grep "full screen" "$DIR/orbit.log" | tail -3
pprof_top /tmp/orbit-cpu-2.prof fullscreen
act fullscreen false; sleep 1
shot 11-retina

# every look, one after another, at Retina size and 60 fps: frames per second when settled, late
# frames, and the longest frame (switching looks rebuilds pictures). Reports every 2 s.
SLOW=""
scen() {
  name=$1; shift
  before=$(grep -c "perf:" "$DIR/orbit.log")
  for kv in "$@"; do set_opt "${kv%%=*}" "${kv#*=}"; done
  sleep 5
  alive "$name"
  lines=$(grep "perf:" "$DIR/orbit.log" | tail -n +$((before + 1)))
  last=$(echo "$lines" | tail -1)
  fps=$(echo "$last" | grep -oE '[0-9.]+ fps' | cut -d' ' -f1)
  late=$(echo "$last" | grep -oE '[0-9]+ late frames' | cut -d' ' -f1)
  long=$(echo "$last" | grep -oE 'longest frame [0-9.]+' | awk '{print $3}')
  sw=$(echo "$lines" | grep -oE 'longest frame [0-9.]+' | awk '{print $3}' | sort -n | tail -1)
  echo "SCEN $(printf '%-28s' "$name") ${fps:-?} fps, ${late:-?} late, longest ${long:-?} ms (switching: ${sw:-?} ms) · $(ps -o rss= -p $PID | awk '{printf "%d MB", $1/1024}') · $(echo "$last" | grep -oE 'memory: .*')"
  awk -v f="${fps:-0}" -v l="${late:-99}" -v t="$FPSWANT" 'BEGIN{exit !(f < t*0.95 || l > 3)}' && SLOW="$SLOW
  $name: ${fps:-?} fps, ${late:-?} late"
}
FPSWANT=60
for k in ShowLyrics ShowVinyl ShowHalo ShowClock ShowWeather; do set_opt $k true; done
set_opt FPS 60; set_opt Motion 3; set_opt Effect 1
for st in $(seq 0 30); do scen "lyrics style $st" LyricsStyle=$st; done
set_opt LyricsStyle 3
for e in $(seq 0 11); do scen "effect $e" Effect=$e; done
set_opt Effect 1
for m in 0 1 2 3 4; do scen "motion $m" Motion=$m; done
set_opt Motion 3
for d in 0 1 2 3 4 5 6; do scen "record $d" DiscStyle=$d; done
scen "record, no ambient light" AmbVinyl=false
set_opt AmbVinyl true
for h in 0 1 2 3 4 5; do scen "visualizer $h" HaloStyle=$h; done
for c in 0 1 2 3 4; do scen "clock $c" ClockStyle=$c; done
set_opt ShowGlow true
for gl in $(seq 0 9); do scen "screen glow $gl" GlowStyle=$gl; done
set_opt ShowGlow false
scen "text size big" TextSize=90
scen "text size small" TextSize=40
FPSWANT=30
set_opt FsFPS 30; act fullscreen true; sleep 2
for c in 0 2; do for b in 0 1 2 3 4; do scen "Cinema cover $c, background $b" FsCover=$c FsBg=$b; done; done
for st in 0 1 4 12 20 28 30 31; do scen "Cinema stage $st" FsStage=$st; done
for m in 0 1 2 3 4; do scen "Cinema motion $m" Motion=$m; done
set_opt FsFPS 60; FPSWANT=60
scen "Cinema 60 fps" FsCover=2 FsBg=1 FsStage=12
for l in tr ru en; do scen "Cinema language $l" Lang="\"$l\""; done
act fullscreen false; sleep 1
FPSWANT=60
# song changes: new cover, new lyrics, every picture rebuilt
for i in 1 2 3 4; do kill -USR2 $PID; scen "song change $i"; done
# quick switching back and forth must not crash or leak
for i in $(seq 1 40); do set_opt LyricsStyle $((i % 31)); set_opt DiscStyle $((i % 7)); set_opt HaloStyle $((i % 6)); sleep 0.15; done
alive "fast switching"
for i in 1 2 3 4 5 6; do act fullscreen true; sleep 0.7; act fullscreen false; sleep 0.4; done
alive "full screen in and out"
echo "memory after all of it: $(ps -o rss= -p $PID | awk '{printf "%d MB", $1/1024}')"
grep "slow frame" "$DIR/orbit.log" | tail -20
if [ -n "$SLOW" ]; then echo "NOT SMOOTH:$SLOW"; else echo "every look ran smoothly"; fi
act quit true; sleep 3
grep -E "closed [0-9]+ other" "$DIR/orbit.log" | tail -1
echo "Orbit (macOS) played without crashing"
