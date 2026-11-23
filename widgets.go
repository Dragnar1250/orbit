//go:build windows || darwin

package main

// Clock, weather and screen glow widgets.

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
)

var fontList []string

/* ---------- shared text painting ---------- */

// spacedPath adds text to a path with extra letter spacing (px); returns the advance width.
func spacedPath(path uintptr, f *Font, s string, x, y, sp float32) float32 {
	if sp == 0 {
		w, _ := measure(f, s)
		if path != 0 {
			addTextPath(path, f, s, x, y)
		}
		return w
	}
	cx := x
	rs := []rune(s)
	for i, r := range rs {
		cw, _ := measure(f, string(r))
		if path != 0 {
			addTextPath(path, f, string(r), cx, y)
		}
		cx += cw
		if i < len(rs)-1 {
			cx += sp
		}
	}
	return cx - x
}

func spacedWidth(f *Font, s string, sp float32) float32 { return spacedPath(0, f, s, 0, 0, sp) }

// clockColors: fill colours (top, bottom) for the clock.
func clockColors() (uint32, uint32) {
	switch cfg.ClockColorMode {
	case 1:
		return parseHex(cfg.ClockColor), parseHex(cfg.ClockColor2)
	case 2:
		a, b := visColors()
		return mixColor(b, 0xFFFFFFFF, 0.2), a
	}
	c := parseHex(cfg.ClockColor)
	return c, c
}

// paintText fills a text path with the clock's colour, shadow and glow.
func paintText(g, path uintptr, alpha float64, size float32, glowBoost float32) {
	c0, c1 := clockColors()
	if cfg.ClockShadow {
		d := float32(math.Max(1.5, float64(size)*0.035))
		gTranslate(g, d*0.4, d)
		fillPathColor(g, path, withAlpha(0xFF000000, 0.42*alpha))
		gTranslate(g, -d*0.4, -d)
	}
	if cfg.ClockGlow || glowBoost > 0 {
		amt := float32(0.3)
		if cfg.ClockGlow {
			amt = 1
		}
		amt += glowBoost
		for i, wm := range []float32{1.0, 0.6, 0.3} {
			strokePath(g, path, withAlpha(c1|0xFF000000, alpha*[]float64{0.06, 0.1, 0.18}[i]*float64(amt)), size*0.28*wm+1)
		}
	}
	if c0 == c1 {
		fillPathColor(g, path, withAlpha(c0, alpha))
		return
	}
	b := pathBounds(path)
	fillPathGradient(g, path, withAlpha(c0, alpha), withAlpha(c1, alpha), b.Y, b.Y+b.H)
}

/* ===================== Clock ===================== */

type ClockView struct {
	w   *Widget
	sig string
}

func newClockView() *ClockView { return &ClockView{w: newWidget("clock", "Clock")} }

func (cv *ClockView) Update(now time.Time, snap MediaSnap, bass float32, ui float64) {
	if !cfg.ShowClock {
		cv.w.release()
		cv.sig = ""
		return
	}
	p := cfg.widget("clock")
	W, H := clockBox(cfg.ClockStyle, cfg.ClockDate, ui*p.Scale)
	wa := workArea()
	x, y := p.X, p.Y
	if !p.Set {
		x, y = int(wa.Right)-W-int(40*ui), int(wa.Top)+int(40*ui)
	}
	sizeChanged := cv.w.w != W || cv.w.h != H
	cv.w.resize(W, H)
	if sizeChanged || layoutDirty || !cv.w.visible {
		cv.w.moveTo(x, y)
	}
	pulse := float32(0)
	if cfg.ClockPulse && snap.Playing && cfg.FPS > 0 {
		pulse = float32(math.Round(math.Min(1, float64(bass)*1.3)*10) / 10)
	}
	tk := now.Format("15:04")
	if cfg.ClockSeconds {
		tk = now.Format("15:04:05")
	}
	sig := fmt.Sprintf("%s|%s|%d|%d|%d|%d|%.2f|%d|%s|%s|%v%v%v%v%v%v|%d|%.1f|%v|%x|%d", tk, now.Format("2006-01-02"), W, H, cfg.ClockStyle, cfg.ClockFont,
		cfg.ClockSpacing, cfg.ClockWeight, cfg.ClockColor, cfg.ClockColor2, cfg.Clock24h, cfg.ClockSeconds, cfg.ClockDate, cfg.ClockShadow, cfg.ClockGlow,
		cfg.ClockDateLong, cfg.ClockAlign, pulse, framed("clock"), albumAccent, cfg.ClockColorMode)
	if cfg.ClockColorMode == 2 {
		sig += fmt.Sprint(cfg.HaloPalette, cfg.HaloColorA, cfg.HaloColorB, cfg.Theme)
	}
	if sig == cv.sig && !styleDirty {
		return
	}
	cv.sig = sig
	cv.draw(now, float32(W), float32(H), pulse)
}

func (cv *ClockView) draw(now time.Time, W, H float32, pulse float32) {
	g := cv.w.g()
	gClear(g, 0)
	if pulse > 0 { // swell on the beat
		s := 1 + 0.04*pulse
		gTranslate(g, W/2, H/2)
		gScale(g, s, s)
		gTranslate(g, -W/2, -H/2)
	}
	fam, bold := fontFor(cfg.ClockFont, cfg.ClockWeight, fontList)
	main, ampm, sec := clockParts(now, cfg.Clock24h, cfg.ClockSeconds)
	date := ""
	if cfg.ClockDate {
		date = formatDate(now, cfg.ClockDateLong)
	}
	pad := H * 0.06
	dateH := float32(0)
	if date != "" && cfg.ClockStyle != ckMinimal {
		dateH = H * 0.17
		if cfg.ClockStyle == ckAnalog {
			dateH = H * 0.14
		}
	}
	bodyH := H - dateH
	alignX := func(contentW float32) float32 {
		switch cfg.ClockAlign {
		case 0:
			return pad
		case 2:
			return W - pad - contentW
		}
		return (W - contentW) / 2
	}
	path := newPath()
	defer freePath(path)
	small := newPath()
	defer freePath(small)
	var mainSize float32

	// fit: shrink the font until the content fits the width
	fit := func(size float32, widthOf func(sz float32) float32) float32 {
		for i := 0; i < 12 && widthOf(size) > W-2*pad; i++ {
			size *= 0.9
		}
		return size
	}
	sp := func(sz float32) float32 { return sz * float32(cfg.ClockSpacing) }

	switch cfg.ClockStyle {
	case ckDigital, ckMinimal:
		big := bodyH * 0.62
		if cfg.ClockStyle == ckMinimal {
			big = H * 0.55
		}
		sfx := strings.TrimSpace(strings.TrimSpace(sec+" ") + " " + ampm)
		if cfg.ClockStyle == ckMinimal {
			main = strings.TrimSpace(main + " " + ampm)
			if sec != "" {
				main = strings.Replace(main, " ", ":"+sec+" ", 1)
				if ampm == "" {
					main += ":" + sec
				}
			}
			sfx = ""
		}
		dateTxt := ""
		if cfg.ClockStyle == ckMinimal && date != "" {
			dateTxt = "  ·  " + date
		}
		width := func(sz float32) float32 {
			f := font(fam, float64(sz), bold)
			w := spacedWidth(f, main, sp(sz))
			if sfx != "" {
				w += sz*0.08 + spacedWidth(font(fam, float64(sz*0.3), true), sfx, 0)
			}
			if dateTxt != "" {
				w += spacedWidth(font(fam, float64(sz*0.42), false), dateTxt, 0)
			}
			return w
		}
		big = fit(big, width)
		mainSize = big
		f := font(fam, float64(big), bold)
		_, th := measure(f, main)
		x := alignX(width(big))
		y := (bodyH - th) / 2
		if cfg.ClockStyle == ckMinimal {
			y = (H - th) / 2
		}
		adv := spacedPath(path, f, main, x, y, sp(big))
		if sfx != "" {
			sf := font(fam, float64(big*0.3), true)
			_, sh := measure(sf, sfx)
			spacedPath(path, sf, sfx, x+adv+big*0.08, y+th*0.78-sh, 0)
		}
		if dateTxt != "" {
			df := font(fam, float64(big*0.42), false)
			_, dh := measure(df, dateTxt)
			spacedPath(small, df, dateTxt, x+adv, y+th*0.72-dh, 0)
		}
	case ckStacked:
		hh, mm := main[:strings.Index(main, ":")], main[strings.Index(main, ":")+1:]
		if len(hh) == 1 {
			hh = "0" + hh
		}
		big := bodyH * 0.46
		big = fit(big, func(sz float32) float32 {
			f := font(fam, float64(sz), bold)
			return float32(math.Max(float64(spacedWidth(f, hh, sp(sz))), float64(spacedWidth(f, mm, sp(sz)))))
		})
		mainSize = big
		f := font(fam, float64(big), bold)
		_, th := measure(f, hh)
		lineH := th * 0.86
		top := (bodyH - lineH*2) / 2
		for i, part := range []string{hh, mm} {
			pw := spacedWidth(f, part, sp(big))
			spacedPath(path, f, part, alignX(pw), top+float32(i)*lineH-th*0.07, sp(big))
		}
		if extra := strings.TrimSpace(sec + " " + ampm); extra != "" {
			sf := font(fam, float64(big*0.2), true)
			ew := spacedWidth(sf, extra, 0)
			spacedPath(small, sf, extra, alignX(ew), top+lineH*2-th*0.02, 0)
		}
	case ckWords:
		top, hour := clockWords(now.Hour(), now.Minute())
		big := bodyH * 0.36
		wf := func(sz float32) float32 {
			return float32(math.Max(float64(spacedWidth(font(fam, float64(sz), true), hour, sp(sz))), float64(spacedWidth(font(fam, float64(sz*0.62), bold), top, sp(sz)))))
		}
		big = fit(big, wf)
		mainSize = big
		itsF := font(fam, float64(big*0.32), false)
		topF := font(fam, float64(big*0.62), bold)
		hourF := font(fam, float64(big), true)
		_, ih := measure(itsF, "it's")
		_, tH := measure(topF, "Xg")
		_, hH := measure(hourF, "Xg")
		total := ih + hH*0.92
		if top != "" {
			total += tH * 0.9
		}
		y := (bodyH - total) / 2
		spacedPath(small, itsF, "it's", alignX(spacedWidth(itsF, "it's", 0)), y, 0)
		y += ih
		if top != "" {
			spacedPath(path, topF, top, alignX(spacedWidth(topF, top, sp(big*0.62))), y, sp(big*0.62))
			y += tH * 0.9
		}
		spacedPath(path, hourF, hour, alignX(spacedWidth(hourF, hour, sp(big))), y-hH*0.06, sp(big))
	case ckAnalog:
		cv.drawAnalog(g, now, W, bodyH)
		mainSize = bodyH * 0.3
	}
	paintText(g, path, 1, mainSize, pulse*0.8)
	paintText(g, small, 0.75, mainSize*0.4, 0)

	if date != "" && cfg.ClockStyle != ckMinimal {
		ds := dateH * 0.58
		df := font(fam, float64(ds), true)
		txt := strings.ToUpper(date)
		dsp := ds * 0.12
		dw := spacedWidth(df, txt, dsp)
		for i := 0; i < 10 && dw > W-2*pad; i++ {
			ds *= 0.9
			df = font(fam, float64(ds), true)
			dsp = ds * 0.12
			dw = spacedWidth(df, txt, dsp)
		}
		dp := newPath()
		spacedPath(dp, df, txt, alignX(dw), bodyH+(dateH-ds*1.3)/2, dsp)
		paintText(g, dp, 0.85, ds, 0)
		freePath(dp)
	}
	gReset(g)
	if framed("clock") {
		editFrame(g, W, H, "clock", "Clock")
	}
	cv.w.present()
}

func (cv *ClockView) drawAnalog(g uintptr, now time.Time, W, H float32) {
	c0, c1 := clockColors()
	S := float32(math.Min(float64(W), float64(H))) * 0.92
	cx, cy := W/2, H/2
	switch cfg.ClockAlign {
	case 0:
		cx = S/2 + W*0.04
	case 2:
		cx = W - S/2 - W*0.04
	}
	r := S / 2
	shadow := func(f func(dx, dy float32, c uint32, a float64)) {
		if cfg.ClockShadow {
			f(r*0.02, r*0.04, 0xFF000000, 0.35)
		}
		if cfg.ClockGlow {
			f(0, 0, c1, -1) // glow pass
		}
		f(0, 0, c0, 1)
	}
	// ring + ticks
	strokeCircle(g, withAlpha(c0, 0.18), cx, cy, r*0.98, float32(math.Max(1, float64(r)*0.012)))
	for i := 0; i < 60; i++ {
		a := float64(i) * math.Pi / 30
		cs, sn := float32(math.Cos(a)), float32(math.Sin(a))
		in, wd, al := r*0.9, r*0.008+0.6, 0.35
		if i%5 == 0 {
			in, wd, al = r*0.8, r*0.028, 0.95
			if i%15 != 0 {
				in, wd, al = r*0.84, r*0.02, 0.8
			}
		}
		line(g, withAlpha(c0, al), wd, cx+cs*in, cy+sn*in, cx+cs*r*0.95, cy+sn*r*0.95)
	}
	sec := float64(now.Second()) + float64(now.Nanosecond())/1e9
	if !cfg.ClockSeconds {
		sec = float64(now.Second())
	}
	mins := float64(now.Minute()) + sec/60
	hrs := float64(now.Hour()%12) + mins/60
	hand := func(frac float64, length, width float32) {
		a := frac*2*math.Pi - math.Pi/2
		cs, sn := float32(math.Cos(a)), float32(math.Sin(a))
		shadow(func(dx, dy float32, c uint32, al float64) {
			if al < 0 {
				line(g, withAlpha(c, 0.15), width*3, cx-cs*r*0.1, cy-sn*r*0.1, cx+cs*length, cy+sn*length)
				return
			}
			line(g, withAlpha(c, al), width, cx+dx-cs*r*0.1, cy+dy-sn*r*0.1, cx+dx+cs*length, cy+dy+sn*length)
		})
	}
	hand(hrs/12, r*0.5, r*0.065)
	hand(mins/60, r*0.74, r*0.04)
	if cfg.ClockSeconds {
		a := sec/60*2*math.Pi - math.Pi/2
		cs, sn := float32(math.Cos(a)), float32(math.Sin(a))
		line(g, c1|0xFF000000, float32(math.Max(1, float64(r)*0.014)), cx-cs*r*0.18, cy-sn*r*0.18, cx+cs*r*0.82, cy+sn*r*0.82)
		fillCircle(g, c1|0xFF000000, cx, cy, r*0.045)
	}
	fillCircle(g, c0, cx, cy, r*0.035)
}

/* ===================== Weather ===================== */

type WeatherView struct {
	w       *Widget
	sig     string
	started time.Time
}

func newWeatherView() *WeatherView {
	return &WeatherView{w: newWidget("weather", "Weather"), started: time.Now()}
}

func (wv *WeatherView) Update(now time.Time, ui float64) {
	if !cfg.ShowWeather {
		wv.w.release()
		wv.sig = ""
		return
	}
	p := cfg.widget("weather")
	W, H := weatherBox(cfg.WeatherStyle, ui*p.Scale)
	wa := workArea()
	x, y := p.X, p.Y
	if !p.Set {
		cw, ch := clockBox(cfg.ClockStyle, cfg.ClockDate, ui*cfg.widget("clock").Scale)
		x, y = int(wa.Right)-W-int(40*ui), int(wa.Top)+int(40*ui)
		if cfg.ShowClock && !cfg.widget("clock").Set {
			y += ch + int(16*ui)
			_ = cw
		}
	}
	sizeChanged := wv.w.w != W || wv.w.h != H
	wv.w.resize(W, H)
	if sizeChanged || layoutDirty || !wv.w.visible {
		wv.w.moveTo(x, y)
	}
	data := weatherSnapshot()
	anim := cfg.WeatherAnimate && cfg.FPS > 0 && data.OK
	t := now.Sub(wv.started).Seconds()
	key := int64(0)
	if anim {
		fps := cfg.FPS
		if fps > 30 {
			fps = 30 // gentle motion: 30 fps is plenty and halves the cost
		}
		key = now.UnixMilli() / int64(1000/fps)
	}
	sig := fmt.Sprintf("%d|%d|%d|%v|%v|%.2f|%s|%v|%v|%d|%s|%x|%s|%s", key, W, H, data.Updated.Unix(), data.OK, cfg.WeatherCard, data.Err, cfg.WeatherDetails,
		cfg.WeatherF, cfg.WeatherStyle, cfg.WeatherCity, textColor(), now.Format("2006-01-02"), fmt.Sprint(framed("weather")))
	if sig == wv.sig && !styleDirty {
		return
	}
	wv.sig = sig
	wv.draw(data, float32(W), float32(H), t, anim)
}

func (wv *WeatherView) draw(d Weather, W, H float32, t float64, anim bool) {
	g := wv.w.g()
	gClear(g, 0)
	txt := textColor()
	k := H / 160
	style := cfg.WeatherStyle
	if style == 1 {
		k = H / 190
	} else if style == 2 {
		k = H / 250
	}
	if cfg.WeatherCard > 0 && style != 1 {
		card := roundRectPath(1, 1, W-2, H-2, 22*k)
		fillPathGradient(g, card, withAlpha(0xFF1A1B24, 0.82*cfg.WeatherCard), withAlpha(0xFF101118, 0.9*cfg.WeatherCard), 0, H)
		strokePath(g, card, withAlpha(0xFFFFFFFF, 0.09*cfg.WeatherCard), 1)
		freePath(card)
	}
	ff := "Segoe UI"
	unit := "°"
	tempStr := func(v float64) string { return fmt.Sprintf("%.0f%s", math.Round(v), unit) }
	if !d.OK {
		drawWeatherIcon(g, wkCloudy, 70*k, H/2, 52*k, t, false, 0.45)
		msg := "Pick a city in the settings"
		if cfg.WeatherCity != "" {
			msg = "Loading weather for " + cfg.WeatherCity + "…"
			if d.Err != "" {
				msg = d.Err
			}
		}
		f := font(ff, float64(15*k), true)
		drawText(g, f, msg, 130*k, H/2-12*k, W-140*k, withAlpha(txt, 0.75))
		wv.frame(g, W, H)
		return
	}
	shadowText := func(f *Font, s string, x, y float32, c uint32) {
		drawText(g, f, s, x+1, y+1.5, W, withAlpha(0xFF000000, 0.35))
		drawText(g, f, s, x, y, W, c)
	}
	switch style {
	case 1: // minimal: icon on top, temperature and place under it, no card
		drawWeatherIcon(g, d.Kind, W/2, 62*k, 54*k, t, anim, 1)
		tf := font(ff+" Light", float64(46*k), false)
		ts := tempStr(d.Temp)
		tw, _ := measure(tf, ts)
		shadowText(tf, ts, (W-tw)/2, 108*k, txt)
		pf := font(ff, float64(13*k), true)
		ps := strings.ToUpper(cityShort(d.Place)) + "  ·  " + strings.ToUpper(d.Text)
		pw, _ := measure(pf, ps)
		if pw > W-10 {
			ps = strings.ToUpper(cityShort(d.Place))
			pw, _ = measure(pf, ps)
		}
		shadowText(pf, ps, (W-pw)/2, 164*k, withAlpha(txt, 0.8))
	default: // card / forecast top
		ic := 50 * k
		drawWeatherIcon(g, d.Kind, 72*k, 80*k, ic, t, anim, 1)
		x := 140 * k
		tf := font(ff+" Light", float64(52*k), false)
		ts := tempStr(d.Temp)
		tw, _ := measure(tf, ts)
		drawText(g, tf, ts, x, 18*k, W, txt)
		cf := font(ff, float64(15*k), true)
		drawText(g, cf, d.Text, x+tw+12*k, 34*k, W-x-tw-16*k, txt)
		if len(d.Days) > 0 {
			hl := fmt.Sprintf("H %s  L %s", tempStr(d.Days[0].Max), tempStr(d.Days[0].Min))
			drawText(g, font(ff, float64(13*k), false), hl, x+tw+12*k, 56*k, W-x-tw-16*k, withAlpha(txt, 0.65))
		}
		pf := font(ff, float64(13*k), true)
		drawText(g, pf, strings.ToUpper(cityShort(d.Place)), x, 92*k, W-x-14*k, withAlpha(txt, 0.85))
		if cfg.WeatherDetails {
			spd := "km/h"
			if d.Unit == "F" {
				spd = "mph"
			}
			det := fmt.Sprintf("Feels %s  ·  %.0f%%  ·  %.0f %s", tempStr(d.Feels), d.Humidity, d.Wind, spd)
			drawText(g, font(ff, float64(12*k), false), det, x, 116*k, W-x-14*k, withAlpha(txt, 0.6))
		}
		if style == 2 && len(d.Days) > 1 {
			line(g, withAlpha(txt, 0.1), 1, 20*k, 156*k, W-20*k, 156*k)
			days := d.Days[1:]
			if len(days) > 4 {
				days = days[:4]
			}
			cw := (W - 30*k) / float32(len(days))
			for i, day := range days {
				cx := 15*k + cw*float32(i) + cw/2
				name := day.Date
				if tm, err := time.Parse("2006-01-02", day.Date); err == nil {
					if s := localDate(tm, "ddd"); s != "" {
						name = s
					} else {
						name = tm.Format("Mon")
					}
				}
				nf := font(ff, float64(12*k), true)
				nw, _ := measure(nf, name)
				drawText(g, nf, name, cx-nw/2, 164*k, cw, withAlpha(txt, 0.75))
				drawWeatherIcon(g, day.Kind, cx, 200*k, 16*k, t, false, 1)
				mm := fmt.Sprintf("%s %s", tempStr(day.Max), tempStr(day.Min))
				mf := font(ff, float64(12*k), false)
				mw, _ := measure(mf, mm)
				drawText(g, mf, mm, cx-mw/2, 222*k, cw, withAlpha(txt, 0.8))
			}
		}
	}
	wv.frame(g, W, H)
}

func (wv *WeatherView) frame(g uintptr, W, H float32) {
	if framed("weather") {
		editFrame(g, W, H, "weather", "Weather")
	}
	wv.w.present()
}

// cityShort: "Konya, Türkiye" stays; very long labels drop the middle part.
func cityShort(s string) string {
	parts := strings.Split(s, ", ")
	if len(parts) > 2 {
		return parts[0] + ", " + parts[len(parts)-1]
	}
	return s
}

/* ---------- weather icons (drawn, animated) ---------- */

func cloudPath(cx, cy, w float32) uintptr {
	p := newPath()
	pathWinding(p) // winding: overlapping puffs make one shape
	h := w * 0.34
	r := roundRectPath(cx-w/2, cy-h*0.1, w, h, h/2)
	addPath(p, r)
	freePath(r)
	for _, c := range [][3]float32{{-0.22, 0.02, 0.2}, {0.04, -0.1, 0.28}, {0.27, 0.03, 0.17}} {
		x, y, rr := cx+c[0]*w, cy+c[1]*w, c[2]*w
		addEllipse(p, x-rr, y-rr, 2*rr, 2*rr)
	}
	return p
}

func drawCloud(g uintptr, cx, cy, w float32, top, bottom uint32, alpha float64) {
	sh := cloudPath(cx+w*0.02, cy+w*0.05, w)
	fillPathColor(g, sh, withAlpha(0xFF000000, 0.18*alpha))
	freePath(sh)
	p := cloudPath(cx, cy, w)
	fillPathGradient(g, p, withAlpha(top, alpha), withAlpha(bottom, alpha), cy-w*0.4, cy+w*0.25)
	freePath(p)
}

func drawSun(g uintptr, cx, cy, r float32, t float64, alpha float64) {
	softGlow(g, cx, cy, r*2.1, withAlpha(0xFFFFB13D, 0.45*alpha))
	rot := t * 14
	for i := 0; i < 10; i++ {
		a := (float64(i)*36 + rot) * math.Pi / 180
		ln := 1.0 + 0.08*math.Sin(t*2.4+float64(i))
		cs, sn := float32(math.Cos(a)), float32(math.Sin(a))
		line(g, withAlpha(0xFFFFC64A, 0.9*alpha), r*0.16, cx+cs*r*1.3, cy+sn*r*1.3, cx+cs*r*float32(1.3+0.35*ln), cy+sn*r*float32(1.3+0.35*ln))
	}
	fillCircle(g, withAlpha(0xFFFFA726, alpha), cx, cy, r)
	softGlow(g, cx-r*0.25, cy-r*0.3, r*0.85, withAlpha(0xFFFFE8A0, 0.9*alpha))
}

func drawMoon(g uintptr, cx, cy, r float32, t float64, alpha float64) {
	softGlow(g, cx, cy, r*1.9, withAlpha(0xFF9FB4FF, 0.3*alpha))
	clip := clipCircle(g, cx, cy, r)
	cut := newPath()
	addEllipse(cut, cx-r*0.35, cy-r*1.25, r*1.8, r*1.8)
	clipExclude(g, cut)
	fillCircle(g, withAlpha(0xFFEEF0F8, alpha), cx, cy, r)
	clipReset(g)
	freePath(cut)
	freePath(clip)
	for i, s := range [][3]float32{{1.25, -0.9, 0.07}, {1.6, -0.2, 0.05}, {0.9, -1.4, 0.045}} {
		tw := 0.5 + 0.5*math.Sin(t*2+float64(i)*2.1)
		fillCircle(g, withAlpha(0xFFFFFFFF, alpha*(0.4+0.6*tw)), cx+s[0]*r, cy+s[1]*r, s[2]*r*float32(0.8+0.4*tw))
	}
}

// drawWeatherIcon draws icon kind centred at (cx, cy), roughly 2s wide; t drives the animation.
func drawWeatherIcon(g uintptr, kind int, cx, cy, s float32, t float64, anim bool, alpha float64) {
	if !anim {
		t = 0.6
	}
	drift := float32(math.Sin(t*0.6)) * s * 0.04
	white, grey := uint32(0xFFF7F8FC), uint32(0xFFC9CED9)
	switch kind {
	case wkClearDay:
		drawSun(g, cx, cy, s*0.55, t, alpha)
	case wkClearNight:
		drawMoon(g, cx, cy, s*0.6, t, alpha)
	case wkPartlyDay, wkPartlyNight:
		if kind == wkPartlyDay {
			drawSun(g, cx-s*0.3, cy-s*0.28, s*0.42, t, alpha)
		} else {
			drawMoon(g, cx-s*0.3, cy-s*0.3, s*0.42, t, alpha)
		}
		drawCloud(g, cx+s*0.12+drift, cy+s*0.22, s*1.35, white, grey, alpha)
	case wkCloudy:
		drawCloud(g, cx+s*0.32-drift, cy-s*0.22, s*1.05, 0xFFB7BDC9, 0xFF8E95A3, alpha)
		drawCloud(g, cx-s*0.12+drift, cy+s*0.16, s*1.45, white, grey, alpha)
	case wkFog:
		drawCloud(g, cx+drift, cy-s*0.2, s*1.4, 0xFFD5D9E2, 0xFFAEB4C0, alpha*0.85)
		for i := 0; i < 3; i++ {
			off := float32(math.Sin(t*0.9+float64(i)*1.7)) * s * 0.12
			y := cy + s*0.32 + float32(i)*s*0.2
			w := s * (1.4 - float32(i)*0.25)
			line(g, withAlpha(0xFFE3E6EE, alpha*(0.75-float64(i)*0.15)), s*0.1, cx-w/2+off, y, cx+w/2+off, y)
		}
	case wkDrizzle, wkRain, wkStorm:
		top, bot := white, grey
		if kind == wkStorm {
			top, bot = 0xFF9AA0AE, 0xFF5E6472
		}
		n, speed, ln := 7, 1.9, s*0.26
		if kind == wkDrizzle {
			n, speed, ln = 5, 1.1, s*0.13
		}
		for i := 0; i < n; i++ {
			ph := math.Mod(t*speed+float64(i)*0.37, 1)
			x := cx - s*0.55 + float32(i)*s*1.1/float32(n-1) - float32(ph)*s*0.12
			y := cy + s*0.28 + float32(ph)*s*0.8
			a := alpha * math.Sin(ph*math.Pi)
			line(g, withAlpha(0xFF6EC1FF, a), float32(math.Max(1.2, float64(s)*0.055)), x, y, x-s*0.06, y+ln)
		}
		drawCloud(g, cx+drift, cy-s*0.12, s*1.5, top, bot, alpha)
		if kind == wkStorm {
			ft := math.Mod(t, 3.4)
			if !anim || ft < 0.12 || (ft > 0.22 && ft < 0.32) {
				bolt := []PointF{{cx + s*0.06, cy + s*0.05}, {cx - s*0.2, cy + s*0.5}, {cx - s*0.02, cy + s*0.5}, {cx - s*0.14, cy + s*0.92},
					{cx + s*0.24, cy + s*0.36}, {cx + s*0.04, cy + s*0.36}, {cx + s*0.16, cy + s*0.05}}
				softGlow(g, cx, cy+s*0.5, s*0.7, withAlpha(0xFFFFE27A, 0.5*alpha))
				fillPoly(g, withAlpha(0xFFFFD84A, alpha), bolt)
			}
		}
	case wkSnow:
		for i := 0; i < 7; i++ {
			ph := math.Mod(t*0.45+float64(i)*0.29, 1)
			x := cx - s*0.55 + float32(i)*s*1.1/6 + float32(math.Sin(t*1.5+float64(i)))*s*0.06
			y := cy + s*0.3 + float32(ph)*s*0.75
			fillCircle(g, withAlpha(0xFFFFFFFF, alpha*math.Sin(ph*math.Pi)), x, y, s*0.055)
		}
		drawCloud(g, cx+drift, cy-s*0.12, s*1.5, white, 0xFFD6DCE6, alpha)
	}
}

/* ===================== Screen glow ===================== */

type glowPart struct {
	w      *Widget
	px, py int
	mon    int
	side   int // 0 top, 1 right, 2 bottom, 3 left
}

type ember struct {
	mon      int
	u, depth float64
	vel, age float64
	life     float64
	size     float32
}

type GlowView struct {
	parts   []*glowPart
	mons    []RECT
	prim    []bool
	monAt   time.Time
	t       float64
	phase   float64
	lv      [nBands]float32
	energy  float32
	comet   float64
	embers  []ember
	rnd     *rand.Rand
	frameAt time.Time
	empty   bool
}

func newGlowView() *GlowView { return &GlowView{rnd: rand.New(rand.NewSource(time.Now().UnixNano()))} }

func (gv *GlowView) hideAll() {
	for _, p := range gv.parts {
		p.w.hide()
	}
}

func (gv *GlowView) sides() []int {
	switch cfg.GlowWhere {
	case 1:
		return []int{2}
	case 2:
		return []int{1, 3}
	case 3:
		return []int{0, 2}
	}
	return []int{0, 1, 2, 3}
}

func (gv *GlowView) Update(now time.Time, snap MediaSnap, bands [nBands]float32, bass float32, ui float64) {
	if !cfg.ShowGlow {
		gv.hideAll()
		return
	}
	fps := cfg.FPS
	if fps > 30 {
		fps = 30 // edges of the whole screen: 30 fps keeps it smooth and light
	}
	if fps == 0 {
		fps = 5
	}
	if now.Sub(gv.frameAt) < time.Second/time.Duration(fps) && !styleDirty {
		return
	}
	dt := math.Min(0.2, now.Sub(gv.frameAt).Seconds())
	gv.frameAt = now
	gv.t += dt
	if now.Sub(gv.monAt) > 3*time.Second || styleDirty {
		gv.mons, gv.prim = monitors()
		gv.monAt = now
	}
	music := gv.step(dt, snap, bands, bass)
	quiet := music && gv.energy < 0.01 && len(gv.embers) == 0
	if quiet && gv.empty && !styleDirty {
		return // nothing to show and already cleared
	}

	T := int(90 * ui * cfg.GlowReach)
	if T < 12 {
		T = 12
	}
	sides := gv.sides()
	n := 0
	for mi, m := range gv.mons {
		if cfg.GlowScreens == 1 && !gv.prim[mi] {
			continue
		}
		show := cfg.GlowFullscreen || !fullscreenOn(m)
		for _, sd := range sides {
			if n >= len(gv.parts) {
				w := newWidget("glow", "Glow")
				w.movable = false
				gv.parts = append(gv.parts, &glowPart{w: w})
			}
			p := gv.parts[n]
			n++
			p.mon, p.side = mi, sd
			if !show {
				p.w.hide()
				continue
			}
			x, y, w, h := gv.rectFor(m, sd, T, sides)
			p.w.resize(w, h)
			if x != p.px || y != p.py || !p.w.visible {
				p.w.moveTo(x, y)
				p.px, p.py = x, y
			}
			gv.drawPart(p, m, x, y, T)
		}
	}
	for ; n < len(gv.parts); n++ {
		gv.parts[n].w.hide()
	}
	gv.empty = quiet
}

// step advances the levels (music or steady rhythm), comets and embers by dt seconds.
func (gv *GlowView) step(dt float64, snap MediaSnap, bands [nBands]float32, bass float32) bool {
	music := cfg.GlowMode == 0
	// levels: the music, or a steady rhythm at the chosen speed
	if music {
		var e float32
		for k := range gv.lv {
			tg := float32(0)
			if snap.Playing {
				tg = float32(math.Min(1, float64(bands[k])*cfg.HaloSens*1.1))
			}
			gv.lv[k] += (tg - gv.lv[k]) * 0.45
			e += gv.lv[k]
		}
		gv.energy += ((e/float32(nBands))*1.8 + bass*0.6 - gv.energy) * 0.4
		if !snap.Playing {
			gv.energy *= 0.85
		}
	} else {
		gv.phase += dt * cfg.GlowSpeed * 2 * math.Pi
		b := float32(0.5 + 0.5*math.Sin(gv.phase))
		for k := range gv.lv {
			gv.lv[k] = float32(0.3 + 0.55*(0.5+0.5*math.Sin(gv.phase-float64(k)*0.18)))
		}
		gv.energy = b
	}
	if gv.energy > 1 {
		gv.energy = 1
	}
	gv.comet += dt * (0.03 + 0.12*float64(gv.energy))
	if !music {
		gv.comet += dt * cfg.GlowSpeed * 0.08
	}
	if cfg.GlowStyle == glEmbers {
		gv.stepEmbers(dt)
	}
	return music
}

// drawAll draws every edge of screen m (in g's own coordinates) — used by the full-screen frame.
func (gv *GlowView) drawAll(g uintptr, m RECT, T float32) {
	for _, sd := range gv.sides() {
		gv.drawSide(g, m, sd, T)
	}
	if cfg.GlowStyle == glComets {
		gv.drawComets(g, 0, m, T)
	}
	if cfg.GlowStyle == glEmbers {
		gv.drawEmbers(g, 0, m, T, m)
	}
}

func hasSide(sides []int, s int) bool {
	for _, x := range sides {
		if x == s {
			return true
		}
	}
	return false
}

func (gv *GlowView) rectFor(m RECT, side, T int, sides []int) (int, int, int, int) {
	mx, my, mw, mh := int(m.Left), int(m.Top), int(m.Right-m.Left), int(m.Bottom-m.Top)
	switch side {
	case 0:
		return mx, my, mw, T
	case 2:
		return mx, my + mh - T, mw, T
	}
	top, bot := 0, 0
	if hasSide(sides, 0) {
		top = T
	}
	if hasSide(sides, 2) {
		bot = T
	}
	if side == 1 {
		return mx + mw - T, my + top, T, mh - top - bot
	}
	return mx, my + top, T, mh - top - bot
}

// edge geometry in screen pixels: point at distance a along side, depth d into the screen
func edgePoint(m RECT, side int, a, d float32) (float32, float32) {
	mx, my, mr, mb := float32(m.Left), float32(m.Top), float32(m.Right), float32(m.Bottom)
	switch side {
	case 0:
		return mx + a, my + d
	case 1:
		return mr - d, my + a
	case 2:
		return mr - a, mb - d // runs right → left so the perimeter goes clockwise
	}
	return mx + d, mb - a
}

func edgeLen(m RECT, side int) float32 {
	if side == 0 || side == 2 {
		return float32(m.Right - m.Left)
	}
	return float32(m.Bottom - m.Top)
}

// perimeter position (0..1, clockwise from the top-left corner) of a point on a side
func perimU(m RECT, side int, a float32) float64 {
	w, h := float64(m.Right-m.Left), float64(m.Bottom-m.Top)
	P := 2 * (w + h)
	off := []float64{0, w, w + h, 2*w + h}[side]
	return (off + float64(a)) / P
}

// level at a perimeter position: bass at the bottom centre, highs at the top centre
func (gv *GlowView) levelAt(m RECT, u float64) float32 {
	w, h := float64(m.Right-m.Left), float64(m.Bottom-m.Top)
	u0 := (w + h + w/2) / (2 * (w + h))
	d := math.Abs(u - u0)
	if d > 0.5 {
		d = 1 - d
	}
	k := int(d * 2 * 44)
	if k >= nBands {
		k = nBands - 1
	}
	return gv.lv[k]
}

func hueColor(h float64) uint32 {
	r, g, b := hslToRgb(math.Mod(h+10, 1), 0.85, 0.58)
	return 0xFF000000 | uint32(clamp255(r))<<16 | uint32(clamp255(g))<<8 | uint32(clamp255(b))
}

// colourAt: glow colour at a perimeter position for a level.
func (gv *GlowView) colourAt(u float64, lv float32) uint32 {
	switch {
	case cfg.GlowStyle == glPrism || cfg.GlowColorMode == 3:
		return hueColor(u + gv.t*0.04)
	case cfg.GlowColorMode == 1:
		a := accent() | 0xFF000000
		if albumAccent != 0 {
			a = albumAccent | 0xFF000000
		}
		return mixColor(a, 0xFFFFFFFF, 0.45*float64(lv))
	case cfg.GlowColorMode == 2:
		c := parseHex(cfg.GlowColor)
		return mixColor(c, 0xFFFFFFFF, 0.45*float64(lv))
	}
	a, b := visColors()
	return mixColor(a, b, float64(lv))
}

func (gv *GlowView) drawPart(p *glowPart, m RECT, wx, wy, T int) {
	g := p.w.g()
	gClear(g, 0)
	gTranslate(g, -float32(wx), -float32(wy))
	win := RECT{int32(wx), int32(wy), int32(wx + p.w.w), int32(wy + p.w.h)}
	for _, sd := range gv.sides() {
		// draw every side that reaches into this window (the corners belong to top / bottom)
		x, y, w, h := gv.rectFor(m, sd, T, []int{})
		r := RECT{int32(x), int32(y), int32(x + w), int32(y + h)}
		if r.Left >= win.Right || win.Left >= r.Right || r.Top >= win.Bottom || win.Top >= r.Bottom {
			continue
		}
		gv.drawSide(g, m, sd, float32(T))
	}
	if cfg.GlowStyle == glComets {
		gv.drawComets(g, p.mon, m, float32(T))
	}
	if cfg.GlowStyle == glEmbers {
		gv.drawEmbers(g, p.mon, m, float32(T), win)
	}
	gReset(g)
	p.w.present()
}

func (gv *GlowView) drawSide(g uintptr, m RECT, side int, T float32) {
	L := edgeLen(m, side)
	br := cfg.GlowBright
	seg := float32(math.Max(10, float64(T)*0.32))
	style := cfg.GlowStyle
	if style == glLED {
		seg = float32(math.Max(14, float64(T)*0.42))
	}
	if style == glBreathe || style == glComets || style == glEmbers {
		// one wash along the whole side
		e := gv.energy
		depth := T * (0.45 + 0.55*e)
		if style != glBreathe {
			e *= 0.35
			depth = T * 0.35
		}
		gv.gradStrip(g, m, side, 0, L, depth, withAlpha(gv.colourAt(perimU(m, side, L/2), e), br*float64(0.15+0.75*e)))
		return
	}
	n := int(L / seg)
	if n < 1 {
		n = 1
	}
	seg = L / float32(n)
	var prevX, prevY float32
	for i := 0; i < n; i++ {
		a := float32(i) * seg
		u := perimU(m, side, a+seg/2)
		lv := gv.levelAt(m, u)
		c := gv.colourAt(u, lv)
		switch style {
		case glSoft, glPrism:
			gv.gradStrip(g, m, side, a, seg+1, T*(0.35+0.65*lv), withAlpha(c, br*float64(0.12+0.88*lv)))
		case glAurora:
			wave := 0.5 + 0.5*math.Sin(float64(a)*0.006+gv.t*0.8+float64(side))
			wave2 := 0.5 + 0.5*math.Sin(float64(a)*0.013-gv.t*0.5)
			c = mixColor(c, hueColor(u*0.5+gv.t*0.02), 0.35)
			gv.gradStrip(g, m, side, a, seg+1, T*float32(0.4+0.6*wave*(0.6+0.4*float64(lv))), withAlpha(c, br*(0.15+0.6*wave2)*(0.4+0.6*float64(lv))))
		case glNeon:
			x1, y1 := edgePoint(m, side, a, 3)
			x2, y2 := edgePoint(m, side, a+seg+0.5, 3)
			al := br * float64(0.25+0.75*lv)
			line(g, withAlpha(c, al*0.12), T*0.42, x1, y1, x2, y2)
			line(g, withAlpha(c, al*0.25), T*0.16, x1, y1, x2, y2)
			line(g, withAlpha(mixColor(c, 0xFFFFFFFF, 0.6), al), 3, x1, y1, x2, y2)
		case glBars:
			h := T * 0.92 * lv
			if h < 2 {
				continue
			}
			gv.gradStrip(g, m, side, a+seg*0.15, seg*0.7, h, withAlpha(c, br*float64(0.5+0.5*lv)))
		case glLED:
			x, y := edgePoint(m, side, a+seg/2, T*0.14)
			r := seg * 0.16 * (0.6 + 0.9*lv)
			al := br * float64(0.25+0.75*lv)
			fillCircle(g, withAlpha(c, al*0.18), x, y, r*2.6)
			fillCircle(g, withAlpha(mixColor(c, 0xFFFFFFFF, 0.5*float64(lv)), al), x, y, r)
		case glScope:
			d := T*0.12 + lv*T*0.6*float32(0.6+0.4*math.Sin(float64(a)*0.05+gv.t*6))
			x, y := edgePoint(m, side, a, d)
			if i > 0 {
				line(g, withAlpha(c, br*0.2), 7, prevX, prevY, x, y)
				line(g, withAlpha(mixColor(c, 0xFFFFFFFF, 0.5), br*0.95), 2, prevX, prevY, x, y)
			}
			prevX, prevY = x, y
		}
	}
}

// gradStrip: a strip along a side from a to a+len, fading from the edge to depth.
func (gv *GlowView) gradStrip(g uintptr, m RECT, side int, a, ln, depth float32, c uint32) {
	if depth < 1 || c>>24 == 0 {
		return
	}
	x0, y0 := edgePoint(m, side, a, 0)
	x1, y1 := edgePoint(m, side, a+ln, depth)
	ex, ey := edgePoint(m, side, a, 0)
	ix, iy := edgePoint(m, side, a, depth)
	rx, ry := math.Min(float64(x0), float64(x1)), math.Min(float64(y0), float64(y1))
	rw, rh := math.Abs(float64(x1-x0)), math.Abs(float64(y1-y0))
	fillRectGrad(g, float32(rx), float32(ry), float32(rw), float32(rh), ex, ey, ix, iy, c, c&0x00FFFFFF)
}

func (gv *GlowView) perimPoint(m RECT, u float64, d float32) (float32, float32, int) {
	w, h := float64(m.Right-m.Left), float64(m.Bottom-m.Top)
	P := 2 * (w + h)
	dist := math.Mod(u, 1) * P
	if dist < 0 {
		dist += P
	}
	for side, ln := range []float64{w, h, w, h} {
		if dist <= ln {
			x, y := edgePoint(m, side, float32(dist), d)
			return x, y, side
		}
		dist -= ln
	}
	x, y := edgePoint(m, 3, float32(h), d)
	return x, y, 3
}

func (gv *GlowView) drawComets(g uintptr, mon int, m RECT, T float32) {
	br := cfg.GlowBright
	for i := 0; i < 3; i++ {
		u0 := gv.comet + float64(i)/3
		for k := 26; k >= 0; k-- {
			u := u0 - float64(k)*0.0035
			x, y, _ := gv.perimPoint(m, u, T*0.12)
			f := 1 - float64(k)/27
			c := gv.colourAt(math.Mod(u, 1), float32(f))
			r := T * float32(0.03+0.07*f) * (1 + gv.energy*0.6)
			fillCircle(g, withAlpha(c, br*f*f*0.9), x, y, r)
		}
		x, y, _ := gv.perimPoint(m, u0, T*0.12)
		softGlow(g, x, y, T*0.45*(1+gv.energy), withAlpha(gv.colourAt(math.Mod(u0, 1), 1), br*0.6))
	}
}

func (gv *GlowView) stepEmbers(dt float64) {
	spawn := 40 * dt * (0.3 + 3*float64(gv.energy))
	for spawn > 0 && len(gv.embers) < 260 {
		if spawn < 1 && gv.rnd.Float64() > spawn {
			break
		}
		spawn--
		for mi := range gv.mons {
			if cfg.GlowScreens == 1 && !gv.prim[mi] {
				continue
			}
			gv.embers = append(gv.embers, ember{mon: mi, u: gv.rnd.Float64(), vel: 25 + 60*gv.rnd.Float64() + 80*float64(gv.energy),
				life: 1.2 + gv.rnd.Float64()*1.4, size: float32(1.5 + gv.rnd.Float64()*2.5)})
		}
	}
	out := gv.embers[:0]
	for _, e := range gv.embers {
		e.age += dt
		e.depth += e.vel * dt
		if e.age < e.life {
			out = append(out, e)
		}
	}
	gv.embers = out
}

func (gv *GlowView) drawEmbers(g uintptr, mon int, m RECT, T float32, win RECT) {
	sides := gv.sides()
	for _, e := range gv.embers {
		if e.mon != mon {
			continue
		}
		x, y, side := gv.perimPoint(m, e.u, float32(e.depth)*T/90)
		if !hasSide(sides, side) {
			continue
		}
		if int32(x) < win.Left-8 || int32(x) > win.Right+8 || int32(y) < win.Top-8 || int32(y) > win.Bottom+8 {
			continue
		}
		f := 1 - e.age/e.life
		c := gv.colourAt(e.u, float32(f))
		fillCircle(g, withAlpha(c, cfg.GlowBright*f*0.35), x, y, e.size*2.4)
		fillCircle(g, withAlpha(mixColor(c, 0xFFFFFFFF, 0.5), cfg.GlowBright*f), x, y, e.size)
	}
}
