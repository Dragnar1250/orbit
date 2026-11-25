package main

// A hidden test mode, only for testing Orbit on a machine without music (it does nothing unless
// the ORBIT_DEMO environment variable is set):
//
//	ORBIT_DEMO=1            made-up songs with covers and synced lyrics instead of the player
//	ORBIT_STRESS=<seconds>  also opens full screen and keeps changing everything (cover / big
//	                        cover / vinyl, records, backgrounds, every lyrics effect, keys, songs,
//	                        closing and opening again), logs the frame time of each step, and
//	                        writes "STRESS DONE" (or HANG with every goroutine) to orbit.log.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var ticks atomic.Int64 // bumped by every frame (the watchdog sees a stuck UI thread)

var (
	pGetGuiResources = user32.NewProc("GetGuiResources")
)

// resources: GDI and USER handles and the Go heap — growing numbers over a long run are a leak.
func resources() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	p := call(pGetCurrentProcess)
	return fmt.Sprintf("canvases %d bitmaps %d images %d fonts %d · gdi %d · heap %.0f MB goroutines %d", liveCanvas.Load(), liveBitmap.Load(), liveImage.Load(), liveFont.Load(),
		call(pGetGuiResources, p, 0), float64(m.HeapAlloc)/1e6, runtime.NumGoroutine())
}

func demoOn() bool { return os.Getenv("ORBIT_DEMO") != "" }

type demoSong struct{ title, artist, album string }

var demoSongs = []demoSong{
	{"Swim", "Chase Atlantic", "Phases"}, {"505", "Arctic Monkeys", "Favourite Worst Nightmare"},
	{"Midnight City", "M83", "Hurry Up, We're Dreaming"}, {"Redbone", "Childish Gambino", "Awaken, My Love!"},
	{"Nights", "Frank Ocean", "Blonde"}, {"Do I Wanna Know?", "Arctic Monkeys", "AM"},
	{"Blinding Lights", "The Weeknd", "After Hours"}, {"Kyoto", "Phoebe Bridgers", "Punisher"},
}

var demoWords = strings.Fields("I'm going back to five oh five if it's a seven hour flight or a forty five minute drive " +
	"if I'm on your mind and I'll be waiting there for you under city lights the night is young we run so far " +
	"there's nothing in the way of what we are tonight let the music play forever and ever more")

// demoLyrics: synced lines, every 2–3.5 s, short and very long ones.
func demoLyrics(seed int) string {
	r := rand.New(rand.NewSource(int64(seed)))
	var b strings.Builder
	t := 1.5
	for t < 200 {
		n := 2 + r.Intn(7)
		if r.Intn(6) == 0 {
			n = 14 + r.Intn(8) // a long line that wraps
		}
		w := make([]string, n)
		for i := range w {
			w[i] = demoWords[r.Intn(len(demoWords))]
		}
		fmt.Fprintf(&b, "[%02d:%05.2f] %s\n", int(t)/60, math.Mod(t, 60), strings.Join(w, " "))
		t += 2 + r.Float64()*1.5
	}
	return b.String()
}

// demoCover: a gradient with a few circles, different for every song.
func demoCover(i, size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	h := float64(i) * 0.37
	c1 := hsl(h, 0.7, 0.55)
	c2 := hsl(h+0.33, 0.6, 0.25)
	type ring struct{ x, y, r float64 }
	rings := []ring{{0.35, 0.4, 0.2}, {0.7, 0.65, 0.12}, {0.5, 0.8, 0.07}}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x)/float64(size), float64(y)/float64(size)
			t := (fx + fy) / 2
			c := [3]float64{c1[0]*(1-t) + c2[0]*t, c1[1]*(1-t) + c2[1]*t, c1[2]*(1-t) + c2[2]*t}
			for _, rg := range rings {
				if d := math.Hypot(fx-rg.x, fy-rg.y); d < rg.r {
					c = [3]float64{c[0] * 0.25, c[1] * 0.25, c[2] * 0.3}
				}
			}
			img.Set(x, y, color.RGBA{uint8(c[0] * 255), uint8(c[1] * 255), uint8(c[2] * 255), 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func hsl(h, s, l float64) [3]float64 {
	h = math.Mod(h, 1)
	f := func(n float64) float64 {
		k := math.Mod(n+h*12, 12)
		a := s * math.Min(l, 1-l)
		return l - a*math.Max(-1, math.Min(math.Min(k-3, 9-k), 1))
	}
	return [3]float64{f(0), f(8), f(4)}
}

var demoNext atomic.Int32 // asks the feeder for the next song

// demoLoop plays the made-up songs: a new one every ORBIT_DEMO_SWITCH seconds (default 14).
func demoLoop() {
	every := 14.0
	if v, err := strconv.ParseFloat(os.Getenv("ORBIT_DEMO_SWITCH"), 64); err == nil && v > 0 {
		every = v
	}
	os.MkdirAll(appDir()+"/cache", 0o755)
	for i, s := range demoSongs {
		key := keyOf(s.title, s.artist)
		b := []byte(fmt.Sprintf(`{"synced":%q,"at":%d}`, demoLyrics(i), time.Now().Unix()))
		os.WriteFile(cachePath(key), b, 0o644)
	}
	covers := map[int][]byte{}
	i := 0
	for {
		s := demoSongs[i%len(demoSongs)]
		media.SetTrack(s.title, s.artist, s.album, "demo")
		key := keyOf(s.title, s.artist)
		media.SetDuration(key, 200)
		start := 20 + float64(i%5)*13
		media.SetPosition(start, true)
		// the cover comes a moment later, like from a real player (sometimes a sharper one after it)
		time.Sleep(time.Duration(80+rand.Intn(300)) * time.Millisecond)
		n := i % len(demoSongs)
		if covers[n] == nil {
			covers[n] = demoCover(n, 640)
		}
		media.SetCover(key, covers[n])
		at := time.Now()
		pauseEvery, _ := strconv.ParseFloat(os.Getenv("ORBIT_DEMO_PAUSE_EVERY"), 64)
		lastPause := time.Now()
		for time.Since(at).Seconds() < every && demoNext.Load() == 0 {
			time.Sleep(250 * time.Millisecond)
			if pauseEvery > 0 && time.Since(lastPause).Seconds() >= pauseEvery { // a pause, 2.5 s long
				media.SetPosition(start+time.Since(at).Seconds(), false)
				time.Sleep(2500 * time.Millisecond)
				at = at.Add(2500 * time.Millisecond)
				lastPause = time.Now()
				media.SetPosition(start+time.Since(at).Seconds(), true)
				continue
			}
			if rand.Intn(80) == 0 { // a pause now and then
				media.SetPosition(start+time.Since(at).Seconds(), false)
				time.Sleep(900 * time.Millisecond)
				at = at.Add(900 * time.Millisecond)
			}
			media.SetPosition(start+time.Since(at).Seconds(), true)
		}
		demoNext.Store(0)
		i++
	}
}

// watchdog: a UI thread that stops for 6 s is a hang — write where every goroutine is and stop.
func watchdog() {
	last, at := ticks.Load(), time.Now()
	for {
		time.Sleep(time.Second)
		if n := ticks.Load(); n != last {
			last, at = n, time.Now()
			continue
		}
		if time.Since(at) > 6*time.Second {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			logf("HANG: no frame for 6 s\n%s", buf[:n])
			os.Exit(3)
		}
	}
}

// stress keeps changing Cinema for the given time, from the UI thread.
func stress(seconds float64) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	end := time.Now().Add(time.Duration(seconds * float64(time.Second)))
	time.Sleep(3 * time.Second)
	step, worst, worstWhat := 0, 0.0, ""
	ms := map[string][]float64{}
	ui := func(f func()) { // run on the UI thread and wait
		done := make(chan struct{})
		runOnUI(func() { defer close(done); f() })
		select {
		case <-done:
		case <-time.After(8 * time.Second):
		}
	}
	ui(func() { openFullscreen() })
	nStage := len(stageNames())
	keys := []int{0x25, 0x27, 0x26, 0x28, 'B', 'V', 'R', 'A', 'E', 'T', 'L', 'C', 'H', 'H', 'Q'}
	phase1 := 2 * (len(discNames) + 1)                      // first: the record, every look, with and without lyrics
	loop, _ := strconv.Atoi(os.Getenv("ORBIT_STRESS_LOOP")) // the same steps again and again (a leak shows as growth)
	for time.Now().Before(end) {
		step++
		if loop > 0 && step > phase1 && (step-phase1)%loop == 1 {
			r = rand.New(rand.NewSource(1))
			runtime.GC()
			logf("loop: %s", resources())
		}
		what := ""
		ui(func() {
			if fs == nil {
				openFullscreen()
				return
			}
			c := cfg
			switch {
			case step <= phase1:
				c.FsCover, c.FsDisc, c.FsLyrics = 2, (step-1)/2, step%2 == 0
				c.FsStage = []int{0, 1 + stScatter}[step%2]
				c.FsBg = step % len(fsBgNames)
			case step%23 == 0:
				closeFullscreen() // and open again next step
			case step%11 == 0:
				demoNext.Store(1) // the next song (the record changes)
			case step%3 == 0:
				fs.cinemaKey(keys[r.Intn(len(keys))])
			default:
				c.FsCover = r.Intn(len(fsCoverNames))
				c.FsBg = r.Intn(len(fsBgNames))
				c.FsStage = r.Intn(nStage)
				c.FsDisc = r.Intn(len(discNames) + 1)
				c.FsEffect = r.Intn(len(effectNames))
				c.FsTextSize = 40 + r.Intn(60)
				c.FsBold = r.Intn(2) == 0
				c.FsLyrics = r.Intn(6) != 0
				c.FsClock = r.Intn(2) == 0
				c.FsFPS = []int{30, 60, 240}[r.Intn(3)]
				c.FsArm = r.Intn(len(fsArmNames))
				if r.Intn(5) == 0 {
					c.DiscStyle = r.Intn(len(discNames))
					c.Worn = r.Intn(2) == 0
				}
			}
			styleDirty, layoutDirty = true, true
			if fs != nil {
				what = fmt.Sprintf("cover=%d bg=%d stage=%d disc=%d lyrics=%v fps=%d", c.FsCover, c.FsBg, c.FsStage, c.FsDisc, c.FsLyrics, c.FsFPS)
			}
		})
		time.Sleep(time.Duration(500+r.Intn(500)) * time.Millisecond)
		ui(func() {
			if fs != nil && what != "" {
				m := fs.frameMs
				logf("stress %d: %s · %.1f ms a frame", step, what, m)
				if step%20 == 0 {
					logf("resources: %s", resources())
				}
				k := fmt.Sprintf("cover=%d stage=%d", cfg.FsCover, cfg.FsStage)
				ms[k] = append(ms[k], m)
				if m > worst {
					worst, worstWhat = m, what
				}
			}
		})
	}
	ui(func() {
		if fs != nil {
			closeFullscreen()
		}
	})
	var b strings.Builder
	for k, v := range ms {
		s := 0.0
		for _, x := range v {
			s += x
		}
		fmt.Fprintf(&b, "\n  %s: %.1f ms (%d)", k, s/float64(len(v)), len(v))
	}
	runtime.GC()
	logf("STRESS DONE: %d steps, worst %.1f ms (%s), %s%s", step, worst, worstWhat, resources(), b.String())
	time.Sleep(500 * time.Millisecond)
	os.Exit(0)
}

func startDemo() bool {
	if !demoOn() {
		return false
	}
	logf("DEMO mode")
	go demoLoop()
	go watchdog()
	if v, err := strconv.ParseFloat(os.Getenv("ORBIT_STRESS"), 64); err == nil && v > 0 {
		go stress(v)
	} else if v := os.Getenv("ORBIT_SHOW"); v != "" {
		go show(v)
	} else if v := os.Getenv("ORBIT_CYCLE"); v != "" {
		go cycleStages(v)
	}
	return true
}

// show: ORBIT_SHOW="FsCover=2,FsStage=12,..." — full screen with these settings, the frame time
// and what each part costs written every 3 s (ORBIT_SHOW_FOR seconds, default 30).
func show(spec string) {
	time.Sleep(2 * time.Second)
	runOnUI(func() {
		v := reflect.ValueOf(cfg).Elem()
		for _, kv := range strings.Split(spec, ",") {
			k, val, _ := strings.Cut(kv, "=")
			fv := v.FieldByName(strings.TrimSpace(k))
			if !fv.IsValid() {
				continue
			}
			switch fv.Kind() {
			case reflect.Int:
				n, _ := strconv.Atoi(val)
				fv.SetInt(int64(n))
			case reflect.Bool:
				fv.SetBool(val == "1" || val == "true")
			case reflect.Float64:
				x, _ := strconv.ParseFloat(val, 64)
				fv.SetFloat(x)
			case reflect.String:
				fv.SetString(val)
			}
		}
		setLang(cfg.Lang)
		styleDirty, layoutDirty = true, true
		if os.Getenv("ORBIT_SHOW_DESKTOP") == "" {
			openFullscreen()
		}
	})
	if k := os.Getenv("ORBIT_SHOW_KEYS"); k != "" { // keys pressed in full screen, one every 1.5 s
		go func() {
			time.Sleep(1500 * time.Millisecond)
			for _, ch := range k {
				ch := ch
				runOnUI(func() {
					if fs != nil {
						fs.cinemaKey(int(ch))
					}
				})
				time.Sleep(1500 * time.Millisecond)
			}
		}()
	}
	dur := 30.0
	if x, err := strconv.ParseFloat(os.Getenv("ORBIT_SHOW_FOR"), 64); err == nil && x > 0 {
		dur = x
	}
	for t := 0.0; t < dur; t += 3 {
		time.Sleep(3 * time.Second)
		runOnUI(func() {
			if fs != nil {
				logf("show: %.1f ms a frame — %s — %s", fs.frameMs, profLine(), frameStats())
			}
		})
	}
	logf("SHOW DONE")
	time.Sleep(300 * time.Millisecond)
	os.Exit(0)
}

// cycleStages: every lyrics effect in turn (ORBIT_CYCLE=<folder>), each for 4 s: a picture of
// the screen (half size) and what a frame cost, per effect.
func cycleStages(dir string) {
	os.MkdirAll(dir, 0o755)
	time.Sleep(2 * time.Second)
	cover := 0
	if v, err := strconv.Atoi(os.Getenv("ORBIT_CYCLE_COVER")); err == nil {
		cover = v
	}
	wait := func(f func()) {
		done := make(chan struct{})
		runOnUI(func() { defer close(done); f() })
		<-done
	}
	desk := os.Getenv("ORBIT_CYCLE_DESKTOP") != "" // the desktop widgets instead of full screen
	if desk {
		wait(func() {
			cfg.ShowVinyl, cfg.ShowLyrics, cfg.ShowList = true, true, true
			styleDirty, layoutDirty = true, true
		})
	} else {
		wait(func() { cfg.FsCover, cfg.FsBg = cover, 0; openFullscreen() })
	}
	names := stageNames()
	for st := 0; st < len(names); st++ {
		if only := os.Getenv("ORBIT_CYCLE_ONLY"); only != "" && !strings.Contains(","+only+",", ","+strconv.Itoa(st)+",") {
			continue
		}
		wait(func() {
			if desk {
				cfg.LyricsStyle = min(st, len(styleNames)-1)
			} else {
				cfg.FsStage = st
			}
			styleDirty = true
		})
		time.Sleep(4 * time.Second)
		wait(func() {
			if desk {
				logf("cycle %d desktop %s: made %d canvases %d bitmaps · %s", st, styleNames[min(st, len(styleNames)-1)], canvasMade.Swap(0), bitmapMade.Swap(0), resources())
				return
			}
			if fs == nil || fs.cin.buf == nil {
				return
			}
			b := fs.cin.buf
			px := pix(b)
			img := image.NewRGBA(image.Rect(0, 0, b.w/2, b.h/2))
			for y := 0; y < b.h/2; y++ {
				for x := 0; x < b.w/2; x++ {
					p := px[(y*2)*b.w+x*2]
					i := img.PixOffset(x, y)
					img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(p>>16), uint8(p>>8), uint8(p), 255
				}
			}
			f, err := os.Create(fmt.Sprintf("%s/stage%02d.png", dir, st))
			if err == nil {
				png.Encode(f, img)
				f.Close()
			}
			logf("cycle %d %s: %s — %s — made %d canvases %d bitmaps", st, names[st], frameStats(), profLine(), canvasMade.Swap(0), bitmapMade.Swap(0))
			ringN = 0
		})
	}
	logf("CYCLE DONE")
	time.Sleep(300 * time.Millisecond)
	os.Exit(0)
}

// demoBands: a made-up spectrum with a beat (120 bpm), for testing what moves with the music.
func demoBands(now time.Time) (b [nBands]float32, bass float32, active bool) {
	if os.Getenv("ORBIT_DEMO_BEAT") == "0" {
		return
	}
	t := float64(now.UnixMilli()) / 1000
	beat := math.Pow(math.Max(0, math.Cos(math.Mod(t*2, 1)*math.Pi)), 6)
	for i := range b {
		f := float64(i) / float64(nBands)
		b[i] = float32(math.Max(0, (1-f)*0.6*beat+0.25*(0.5+0.5*math.Sin(t*3+f*9))*(1-f*0.5)))
	}
	return b, float32(0.2 + 0.8*beat), true
}

// deskProf: in the test mode, the time each desktop widget takes, logged every 5 s.
var deskP = map[string]time.Duration{}
var deskN = map[string]int{}
var deskAt time.Time

func deskProf(name string, t0 time.Time) {
	if !demoOn() {
		return
	}
	deskP[name] += time.Since(t0)
	deskN[name]++
	if deskAt.IsZero() {
		deskAt = time.Now()
	}
	if time.Since(deskAt) > 5*time.Second {
		line := ""
		for k, v := range deskP {
			line += fmt.Sprintf("  %s %d× %.2f ms", k, deskN[k], float64(v.Microseconds())/1000/float64(max(1, deskN[k])))
		}
		logf("desk:%s over %.1fs", line, time.Since(deskAt).Seconds())
		deskP, deskN, deskAt = map[string]time.Duration{}, map[string]int{}, time.Now()
	}
}

var secAt time.Time

// secProf: in the test mode, the time since the last mark goes to name ("" just starts).
func secProf(name string) {
	if !demoOn() {
		return
	}
	if name != "" {
		deskP[name] += time.Since(secAt)
		deskN[name]++
		if deskAt.IsZero() {
			deskAt = time.Now()
		}
		if time.Since(deskAt) > 5*time.Second {
			line := ""
			for k, v := range deskP {
				line += fmt.Sprintf("  %s %d× %.2f ms", k, deskN[k], float64(v.Microseconds())/1000/float64(max(1, deskN[k])))
			}
			logf("desk:%s over %.1fs", line, time.Since(deskAt).Seconds())
			deskP, deskN, deskAt = map[string]time.Duration{}, map[string]int{}, time.Now()
		}
	}
	secAt = time.Now()
}
