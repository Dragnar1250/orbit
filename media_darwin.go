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

func running(name string) bool { return exec.Command("/usr/bin/pgrep", "-xq", name).Run() == nil }

func osascript(script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script).Output()
	return string(out), err
}

func macMediaLoop() {
	var lastKey string
	denied := map[string]bool{}
	for {
		var sp, mu macNowPlaying
		var spOK, muOK bool
		if running("Spotify") {
			out, err := osascript(macSpotifyScript)
			if err != nil && !denied["spotify"] {
				denied["spotify"] = true
				logf("Spotify didn't answer (allow Orbit in System Settings → Privacy & Security → Automation): %v", err)
			}
			sp, spOK = parseMacNowPlaying(out)
		}
		if running("Music") {
			out, err := osascript(macMusicScript)
			if err != nil && !denied["music"] {
				denied["music"] = true
				logf("Music didn't answer (Automation permission?): %v", err)
			}
			mu, muOK = parseMacNowPlaying(out)
		}
		n, ok := pickMacPlayer(sp, mu, spOK, muOK)
		if !ok {
			media.SetTrack("", "", "", "")
			lastKey = ""
			time.Sleep(1500 * time.Millisecond)
			continue
		}
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
		time.Sleep(700 * time.Millisecond)
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
