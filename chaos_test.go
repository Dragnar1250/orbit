package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSettingsChaos throws thousands of random, broken and hostile requests at the settings server
// from several goroutines while a fake UI thread keeps reading the settings — nothing may panic,
// hang or leave a value outside its range.
func TestSettingsChaos(t *testing.T) {
	cfg = defaultConfig()
	var hookCalls int64
	hooks.SettingsChanged = func(string) { atomic.AddInt64(&hookCalls, 1) }
	hooks.Fullscreen = func(bool) { atomic.AddInt64(&hookCalls, 1) }
	hooks.SetWidget = func(string, float64, float64, float64) {}
	hooks.SetEditMode = func(bool) {}
	hooks.ResetPositions = func(string) {}
	srv, err := startSettingsServer("")
	if err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", srv.port)
	tok := "?t=" + srv.token

	var keys []string
	rt := reflect.TypeOf(Config{})
	for i := 0; i < rt.NumField(); i++ {
		keys = append(keys, rt.Field(i).Name)
	}
	keys = append(keys, "Nope", "", "__proto__", "widgets", "FsKind ")
	junk := []string{`0`, `-1`, `1e309`, `-1e308`, `99999999999`, `0.5`, `true`, `false`, `null`, `""`, `"x"`, `"#GGGGGG"`, `"#FF00FF"`,
		`[]`, `{}`, `[1,2]`, `{"a":1}`, `"` + strings.Repeat("é", 5000) + `"`, `NaN`, `1/0`, `"\u0000"`, `-0`, `3`, `7`, `60`, `30`, `200`}
	actions := []string{"fullscreen", "widget", "widgetFlag", "opacity", "flash", "editMode", "resetPositions", "weatherPlace", "weatherRefresh", "nope", ""}

	stop := make(chan struct{})
	var ui sync.WaitGroup
	ui.Add(1)
	go func() { // the "UI thread": runs queued work and reads everything, like a frame would
		defer ui.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			drainUI()
			withCfg(func() {
				_ = cfg.FsStage + len(cfg.FsFont)
				_ = stageNames()[cfg.FsStage] + fsScreenNames[cfg.FsScreen] + themes[cfg.FsTheme].Name
			})
			time.Sleep(time.Millisecond)
		}
	}()
	client := &http.Client{Timeout: 5 * time.Second}
	var wg sync.WaitGroup
	var failures int64
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))
			for i := 0; i < 400; i++ {
				var body, path string
				switch r.Intn(4) {
				case 0, 1:
					path = "/api/set"
					body = fmt.Sprintf(`{"key":%q,"value":%s}`, keys[r.Intn(len(keys))], junk[r.Intn(len(junk))])
				case 2:
					path = "/api/action"
					body = fmt.Sprintf(`{"name":%q,"value":%s}`, actions[r.Intn(len(actions))], junk[r.Intn(len(junk))])
				default:
					path = "/api/state"
				}
				if r.Intn(20) == 0 {
					body = `{"key":` // cut off
				}
				var resp *http.Response
				var err error
				if body == "" {
					resp, err = client.Get(base + path + tok)
				} else {
					resp, err = client.Post(base+path+tok, "application/json", strings.NewReader(body))
				}
				if err != nil {
					atomic.AddInt64(&failures, 1)
					continue
				}
				if resp.StatusCode >= 500 {
					atomic.AddInt64(&failures, 1)
				}
				resp.Body.Close()
			}
		}(int64(w + 1))
	}
	wg.Wait()
	close(stop)
	ui.Wait()
	drainUI() // leave no queued work behind for the next test
	if failures > 0 {
		t.Fatalf("%d requests failed or crashed the handler", failures)
	}
	// every value is still inside its range: validating again changes nothing
	withCfg(func() {
		before, _ := json.Marshal(cfg)
		c := *cfg
		c.validateFields()
		after, _ := json.Marshal(&c)
		if string(before) != string(after) {
			t.Fatalf("settings left out of range:\n%s\n%s", before, after)
		}
	})
	// the server still answers normally
	resp, err := client.Get(base + "/api/state" + tok)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("server unhealthy after the storm: %v", err)
	}
	resp.Body.Close()
}

// TestValidateRandomConfigs: any config file content becomes a usable config.
func TestValidateRandomConfigs(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	rt := reflect.TypeOf(Config{})
	for i := 0; i < 3000; i++ {
		c := defaultConfig()
		v := reflect.ValueOf(c).Elem()
		for k := 0; k < 12; k++ {
			f := v.Field(r.Intn(rt.NumField()))
			switch f.Kind() {
			case reflect.Int:
				f.SetInt(int64(r.Intn(2000) - 1000))
			case reflect.Float64:
				f.SetFloat([]float64{-1e9, -1, 0, 0.5, 3, 1e9}[r.Intn(6)])
			case reflect.String:
				f.SetString([]string{"", "#", "zz", "#12345", "#ABCDEF", strings.Repeat("a", 999)}[r.Intn(6)])
			case reflect.Bool:
				f.SetBool(r.Intn(2) == 0)
			}
		}
		c.validateFields()
		_ = stageNames()[c.FsStage] + styleNames[c.LyricsStyle] + effectNames[c.FsEffect] + fsScreenNames[c.FsScreen]
		_ = themes[c.FsTheme].Name + themes[c.Theme].Name + uiThemeNames[c.UITheme]
		if c.FsFPS != 30 && c.FsFPS != 60 || c.FsTextSize < 20 || c.FsTextSize > 200 {
			t.Fatalf("out of range: fps %d size %d", c.FsFPS, c.FsTextSize)
		}
	}
}

func FuzzParseLRC(f *testing.F) {
	f.Add("[00:12.34]hello\n[00:15.00]<00:15.10>word <00:15.40>two")
	f.Add("[99:99.99]")
	f.Fuzz(func(t *testing.T, s string) {
		lines := parseLRC(s)
		for i := 1; i < len(lines); i++ {
			if lines[i].T < lines[i-1].T {
				t.Fatalf("lines out of order")
			}
		}
	})
}

// TestLayoutChaos: the layout maths with odd sizes and counts — empty songs, tiny windows,
// huge words — never panics, loops forever or returns NaN.
func TestLayoutChaos(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Logf("seed %d", seed)
	r := rand.New(rand.NewSource(seed))
	bad := func(v float64) bool { return v != v || v > 1e12 || v < -1e12 }
	for i := 0; i < 4000; i++ {
		n := r.Intn(60)
		W, H := float64(r.Intn(3000)-50), float64(r.Intn(2000)-50)
		widths := make([]float64, n)
		for k := range widths {
			widths[k] = []float64{0, 1, r.Float64() * 400, 1e6}[r.Intn(4)]
		}
		maxW := []float64{0, -5, 1, 300, 1e9}[r.Intn(5)]
		rows := balanceRows(widths, 10, maxW)
		seen := 0
		for _, row := range rows {
			seen += len(row)
		}
		if seen != n {
			t.Fatalf("balanceRows lost words: %d of %d", seen, n)
		}
		_ = wrapWords(widths, 10, maxW)
		for _, s := range scatterLayout(n, int64(i), W, H) {
			if bad(float64(s.X)) || bad(float64(s.Y)) {
				t.Fatal("scatter NaN")
			}
		}
		for _, s := range constellationLayout(n, int64(i), W, H, float64(r.Intn(100))) {
			if bad(s.X) || bad(s.Y) {
				t.Fatal("constellation NaN")
			}
		}
		area := rectI{0, 0, r.Intn(500) - 20, r.Intn(500) - 20}
		x, y := randomSpot(r, area, r.Intn(800), r.Intn(800), []rectI{{0, 0, 100, 100}}, [2]int{1, 1})
		_, _ = x, y
		for _, sh := range []int{-1, 0, 3, 99} {
			_ = starPoints(sh, float64(r.Intn(50)))
		}
		a, b := ballHop(0, 0, W, H, r.Float64()*1.4-0.2, float64(r.Intn(100)))
		if bad(a) || bad(b) {
			t.Fatal("ballHop NaN")
		}
		_ = toolbarLayout(r.Intn(8), W, H, float64(r.Intn(60)), 4)
		_ = cornerFor(W/2, H/2, 0, 0, W, H)
		_ = ambLevel(r.Intn(9)-2, r.Float64()*1e4, r.Float64())
		ls := make([]Line, n)
		for k := range ls {
			ls[k] = Line{T: float64(k), End: float64(k + 1), Text: "x"}
		}
		p := r.Float64()*100 - 10
		if j := lineAt(ls, p); j >= n {
			t.Fatal("lineAt out of range")
		}
		L := &Line{T: 0, Sing: 1}
		for k := 0; k < r.Intn(5); k++ {
			L.Words = append(L.Words, Word{W: "w", T: float64(k) * 0.3})
		}
		if w := wordAt(L, p); w >= len(L.Words) && len(L.Words) > 0 {
			t.Fatal("wordAt out of range")
		}
		for k := range L.Words {
			if d := wordDur(L, k); bad(d) || d < 0 {
				t.Fatal("wordDur", d)
			}
		}
		_ = cinemaLayout(float64(r.Intn(4000)+1), float64(r.Intn(3000)+1), r.Intn(2) == 0, r.Intn(2) == 0)
	}
}
