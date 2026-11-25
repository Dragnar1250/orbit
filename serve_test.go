package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// TestServeForUI runs the settings server for browser tests (only when ORBIT_SERVE is set).
func TestServeForUI(t *testing.T) {
	out := os.Getenv("ORBIT_SERVE")
	if out == "" {
		t.Skip("set ORBIT_SERVE=<dir> to run")
	}
	cfg = defaultConfig()
	media.SetTrack("Swim", "Chase Atlantic", "Phases", "smtc")
	media.SetPosition(42, true)
	{ // a fake album cover: purple → orange diagonal with a dark circle
		img := image.NewRGBA(image.Rect(0, 0, 96, 96))
		for y := 0; y < 96; y++ {
			for x := 0; x < 96; x++ {
				t := float64(x+y) / 190
				c := color.RGBA{uint8(90 + 150*t), uint8(40 + 60*t), uint8(160 - 120*t), 255}
				if (x-48)*(x-48)+(y-48)*(y-48) < 400 {
					c = color.RGBA{20, 16, 30, 255}
				}
				img.Set(x, y, c)
			}
		}
		var buf bytes.Buffer
		png.Encode(&buf, img)
		media.SetCover(media.Snapshot().Track.Key, buf.Bytes())
	}
	fm := fakeMeteo(t, 2, nil) // partly cloudy, Konya / Paris searchable
	defer fm.Close()
	geoBase, weatherBase = fm.URL, fm.URL
	go weatherLoop(func() (string, float64, float64, bool, bool) {
		var place string
		var lat, lon float64
		var f, want bool
		withCfg(func() {
			place, lat, lon, f, want = cfg.WeatherCity, cfg.WeatherLat, cfg.WeatherLon, cfg.WeatherF, cfg.ShowWeather
		})
		return place, lat, lon, f, want
	}, func() {})
	var edit, startup atomic.Bool
	actions := []string{}
	widgets := map[string]WidgetInfo{"lyrics": {X: 24, Y: 22, Scale: 1, Visible: true}, "vinyl": {X: 15, Y: 80, Scale: 1, Visible: true}, "halo": {X: 50, Y: 50, Scale: 1, Visible: true},
		"clock": {X: 85, Y: 10, Scale: 1, Visible: true}, "weather": {X: 85, Y: 25, Scale: 1, Visible: true}}
	hooks = Hooks{
		Fonts: func() []string {
			return []string{"Arial", "Consolas", "DejaVu Sans", "DejaVu Serif", "Georgia", "Impact", "Segoe UI", "Bahnschrift"}
		},
		StartupEnabled: startup.Load,
		SetStartup:     func(b bool) { startup.Store(b); actions = append(actions, "startup") },
		OpenFolder:     func() { actions = append(actions, "openFolder") },
		Quit:           func() { actions = append(actions, "quit") },
		SetEditMode:    func(b bool) { edit.Store(b); actions = append(actions, "edit") },
		EditMode:       edit.Load,
		SettingsChanged: func(k string) {
			if k == "WeatherF" || k == "ShowWeather" || k == "WeatherCity" {
				kickWeather()
			}
			if k == "Lang" {
				setLang(cfg.Lang)
			}
		},
		ResetPositions: func(w string) { actions = append(actions, "reset:"+w) },
		WidgetInfo: func() map[string]WidgetInfo {
			out := map[string]WidgetInfo{}
			withCfg(func() {
				for k, v := range widgets {
					out[k] = v
				}
			})
			return out
		},
		SetWidget: func(name string, x, y, sc float64) {
			withCfg(func() {
				o := widgets[name]
				widgets[name] = WidgetInfo{X: x, Y: y, Scale: sc, Visible: true, OnTop: o.OnTop, Locked: o.Locked}
			})
			actions = append(actions, "widget:"+name)
		},
		SetWidgetFlag: func(name, flag string, on bool) {
			withCfg(func() {
				o := widgets[name]
				if flag == "onTop" {
					o.OnTop = on
				} else {
					o.Locked = on
				}
				widgets[name] = o
			})
			actions = append(actions, fmt.Sprintf("flag:%s:%s:%v", name, flag, on))
		},
		Flash:      func(name string) { actions = append(actions, "flash:"+name) },
		Fullscreen: func(open bool) { actions = append(actions, fmt.Sprintf("fullscreen:%v", open)) },
		SetOpacity: func(name string, v float64) {
			withCfg(func() { o := widgets[name]; o.Opacity = v; widgets[name] = o })
			actions = append(actions, fmt.Sprintf("opacity:%s:%.2f", name, v))
		},
	}
	srv, err := startSettingsServer("")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(out+"/url.txt", []byte(srv.URL()), 0o644)
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		drainUI()
		var b []byte
		withCfg(func() {
			b, _ = json.Marshal(map[string]any{"cfg": cfg, "edit": edit.Load(), "startup": startup.Load(), "actions": actions, "widgets": widgets})
		})
		os.WriteFile(out+"/state.json.tmp", b, 0o644)
		os.Rename(out+"/state.json.tmp", out+"/state.json")
		if _, err := os.Stat(out + "/stop"); err == nil {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
}
