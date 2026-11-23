//go:build darwin

package main

import (
	"context"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

var macApp string // "spotify" / "music": the one last seen playing (the menu's controls go there)

var mediaPing = make(chan struct{}, 1) // Spotify / Music announced a change

func running(name string) bool { return exec.Command("/usr/bin/pgrep", "-xq", name).Run() == nil }

func osascript(script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script).Output()
	return string(out), err
}

func jxa(script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script).Output()
	return string(out), err
}

// macMediaLoop asks the players when they announce a change, otherwise only now and then to keep
// the position exact: every 4 s while playing, 8 s when paused or nothing plays.
func macMediaLoop() {
	var lastKey string
	warned := false
	for {
		out, err := jxa(macPlayersScript)
		if err != nil && !warned {
			warned = true
			logf("the music apps didn't answer (allow Orbit in System Settings → Privacy & Security → Automation): %v", err)
		}
		n, ok := parseMacPlayers(out)
		wait := 8 * time.Second
		if !ok {
			media.SetTrack("", "", "", "")
			lastKey = ""
		} else {
			macApp = n.App
			media.SetTrack(n.Title, n.Artist, n.Album, n.App)
			key := keyOf(n.Title, n.Artist)
			if n.Dur > 0 {
				media.SetDuration(key, n.Dur)
			}
			media.SetPosition(n.Pos, n.State == "playing")
			if key != lastKey {
				lastKey = key
				go macCover(key, n)
			}
			if n.State == "playing" {
				wait = 4 * time.Second
			}
		}
		select {
		case <-mediaPing:
			time.Sleep(150 * time.Millisecond) // the player finishes its own update first
		case <-time.After(wait):
		}
	}
}

func macCover(key string, n macNowPlaying) {
	var b []byte
	if n.Art != "" {
		cl := &http.Client{Timeout: 10 * time.Second}
		if resp, err := cl.Get(n.Art); err == nil {
			if resp.StatusCode == 200 {
				b, _ = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			}
			resp.Body.Close()
		}
	}
	if len(b) == 0 {
		b = fetchCoverOnline(n.Title, n.Artist)
	}
	if len(b) > 0 {
		media.SetCover(key, b)
	}
}

// macControl: play / pause, next, previous in the app that plays.
func macControl(cmd string) {
	app := map[string]string{"spotify": "Spotify", "music": "Music"}[macApp]
	if app == "" {
		if running("Spotify") {
			app = "Spotify"
		} else if running("Music") {
			app = "Music"
		} else {
			return
		}
	}
	switch cmd {
	case "playpause", "next track", "previous track":
	default:
		return
	}
	go osascript(`tell application "` + app + `" to ` + strings.TrimSpace(cmd))
}
