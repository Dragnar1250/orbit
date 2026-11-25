package main

import (
	"math"
	"math/rand"
	"strings"
	"unicode"
)

// wrapWords splits words (by width) into rows no wider than maxW.
func wrapWords(widths []float64, space, maxW float64) [][]int {
	var rows [][]int
	var cur []int
	w := 0.0
	for i, ww := range widths {
		add := ww
		if len(cur) > 0 {
			add += space
		}
		if len(cur) > 0 && w+add > maxW {
			rows = append(rows, cur)
			cur, w = nil, 0
			add = ww
		}
		cur = append(cur, i)
		w += add
	}
	if len(cur) > 0 {
		rows = append(rows, cur)
	}
	return rows
}

// balanceRows re-wraps with a smaller width when the last row is very short (nicer centred text).
func balanceRows(widths []float64, space, maxW float64) [][]int {
	rows := wrapWords(widths, space, maxW)
	if len(rows) < 2 {
		return rows
	}
	total := 0.0
	for _, w := range widths {
		total += w + space
	}
	target := total/float64(len(rows)) + space*2
	if target < maxW {
		if r2 := wrapWords(widths, space, target); len(r2) == len(rows) {
			return r2
		}
	}
	return rows
}

var stopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields("a an the and or of to in on at by for is it i me my you your we our he she his her they them be am are was were do so but if as up no oh ah la na de da ve ile bu şu o ben sen bir mi mı mu mü ki ya ne im i'm") {
		stopWords[w] = true
	}
}

func isSmallWord(w string) bool {
	clean := strings.ToLower(strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' }))
	letters := 0
	for _, r := range clean {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return stopWords[clean] || letters <= 2
}

type scatterSpot struct {
	X, Y, Scale, Rot float64
}

// scatterLayout places n words in a W×H box on a shuffled grid (stable for a given seed).
func scatterLayout(n int, seed int64, W, H float64) []scatterSpot {
	if n <= 0 {
		return nil
	}
	r := rand.New(rand.NewSource(seed))
	// a zero / negative / NaN box made cols infinite → int overflow → no cells → crash (found by
	// TestLayoutChaos): keep the box sane and the grid between 1 and n columns
	if !(W >= 1) {
		W = 1
	}
	if !(H >= 1) {
		H = 1
	}
	cols := int(math.Min(float64(n), math.Max(1, math.Round(math.Sqrt(float64(n)*W/H)))))
	rows := int(math.Ceil(float64(n) / float64(cols)))
	cells := make([][2]int, 0, rows*cols)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			cells = append(cells, [2]int{x, y})
		}
	}
	r.Shuffle(len(cells), func(i, j int) { cells[i], cells[j] = cells[j], cells[i] })
	cw, ch := W/float64(cols), H/float64(rows)
	out := make([]scatterSpot, n)
	for i := 0; i < n; i++ {
		c := cells[i]
		out[i] = scatterSpot{
			X:     (float64(c[0]) + 0.5 + (r.Float64()-0.5)*0.35) * cw,
			Y:     (float64(c[1]) + 0.5 + (r.Float64()-0.5)*0.35) * ch,
			Scale: 0.6 + r.Float64()*0.6,
			Rot:   (r.Float64() - 0.5) * 14,
		}
	}
	return out
}

type rectI struct{ L, T, R, B int }

func (a rectI) overlaps(b rectI) bool { return a.L < b.R && b.L < a.R && a.T < b.B && b.T < a.B }

// randomSpot picks a top-left for a w×h box inside area, avoiding the given rects and the last spot.
func randomSpot(r *rand.Rand, area rectI, w, h int, avoid []rectI, last [2]int) (int, int) {
	bestX, bestY, bestScore := area.L, area.T, -1.0
	for i := 0; i < 40; i++ {
		maxX, maxY := area.R-w, area.B-h
		if maxX < area.L {
			maxX = area.L
		}
		if maxY < area.T {
			maxY = area.T
		}
		x := area.L + r.Intn(maxX-area.L+1)
		y := area.T + r.Intn(maxY-area.T+1)
		box := rectI{x, y, x + w, y + h}
		score := 1.0
		for _, a := range avoid {
			if box.overlaps(a) {
				score -= 1
			}
		}
		// prefer spots away from the previous line so it feels random, not jittery
		dist := math.Hypot(float64(x-last[0]), float64(y-last[1]))
		score += math.Min(1, dist/float64(area.R-area.L+1)*3) * 0.5
		if score > bestScore {
			bestX, bestY, bestScore = x, y, score
		}
		if score >= 1.45 {
			break
		}
	}
	return bestX, bestY
}

func easeOutBounce(t float64) float64 {
	const n, d = 7.5625, 2.75
	switch {
	case t < 1/d:
		return n * t * t
	case t < 2/d:
		t -= 1.5 / d
		return n*t*t + 0.75
	case t < 2.5/d:
		t -= 2.25 / d
		return n*t*t + 0.9375
	}
	t -= 2.625 / d
	return n*t*t + 0.984375
}

// wordDur is how long word i is sung (until the next word, or the end of the sung part).
func wordDur(L *Line, i int) float64 {
	if i < 0 || i >= len(L.Words) {
		return 1
	}
	end := L.T + math.Max(0.4, L.Sing)
	if i+1 < len(L.Words) {
		end = L.Words[i+1].T
	}
	return math.Max(0.15, end-L.Words[i].T)
}

func hash32(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}

const glyphs = "#$%&*+=?@<>/ABCDEFGHJKLMNPQRSTUVWXYZ0123456789"

// scramble keeps the first k runes and replaces the rest (punctuation kept) with random glyphs.
func scramble(rs []rune, k int, seed uint32) string {
	out := make([]rune, len(rs))
	for j, r := range rs {
		if j < k || r == ' ' || r == '\'' || r == ',' || r == '.' {
			out[j] = r
			continue
		}
		out[j] = rune(glyphs[hash32(seed*2654435761+uint32(j))%uint32(len(glyphs))])
	}
	return string(out)
}

func lum(c uint32) float64 {
	r, g, b := float64(c>>16&255), float64(c>>8&255), float64(c&255)
	return (0.2126*r + 0.7152*g + 0.0722*b) / 255
}

/* ---------- Constellation ---------- */

type starNode struct {
	X, Y float64
	Link int // an extra line to this earlier node (-1 = none); every node also links to the one before
}

// constellationLayout places n stars on a wandering path inside a W×H box, the same every time for a
// seed. Each new star keeps a sensible distance from the others; every third one also links back to
// its nearest older star so a web grows, like a constellation.
func constellationLayout(n int, seed int64, W, H, margin float64) []starNode {
	r := rand.New(rand.NewSource(seed))
	nodes := make([]starNode, 0, n)
	x0, x1, y0, y1 := margin, W-margin, margin, H-margin*1.6
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	heading := 0.0
	x, y := x0+(x1-x0)*0.08, y0+(y1-y0)*(0.3+0.4*r.Float64())
	step := math.Max(margin*1.2, (x1-x0)*0.11)
	for i := 0; i < n; i++ {
		if i > 0 {
			best, bestD := [2]float64{}, -1.0
			for try := 0; try < 8; try++ {
				a := heading + (r.Float64()*2-1)*1.15
				l := step * (0.7 + 0.6*r.Float64())
				nx, ny := x+math.Cos(a)*l, y+math.Sin(a)*l
				if nx < x0 || nx > x1 {
					nx = math.Max(x0, math.Min(x1, x-math.Cos(a)*l))
				}
				if ny < y0 || ny > y1 {
					ny = math.Max(y0, math.Min(y1, y-math.Sin(a)*l))
				}
				d := math.MaxFloat64 // distance to the closest existing star: keep them apart
				for _, o := range nodes {
					d = math.Min(d, math.Hypot(o.X-nx, o.Y-ny))
				}
				if d > bestD {
					best, bestD = [2]float64{nx, ny}, d
				}
			}
			x, y = best[0], best[1]
			if x > x0+(x1-x0)*0.88 { // reached the right side: wander back
				heading = math.Pi
			} else if x < x0+(x1-x0)*0.12 {
				heading = 0
			}
		}
		link := -1
		if i >= 3 && i%3 == 0 {
			bd := math.MaxFloat64
			for j := 0; j < i-1; j++ {
				if d := math.Hypot(nodes[j].X-x, nodes[j].Y-y); d < bd {
					bd, link = d, j
				}
			}
		}
		nodes = append(nodes, starNode{x, y, link})
	}
	return nodes
}

var starShapeNames = []string{"Sparkle", "Star", "Diamond", "Orb", "Cross", "Heart", "Mixed"}

// starPoints: the outline of a star symbol of radius r around (0,0). Orb and Cross return nil
// (drawn as circles / lines).
func starPoints(shape int, r float64) [][2]float64 {
	var pts [][2]float64
	spikes := func(n int, inner float64) {
		for k := 0; k < 2*n; k++ {
			a := -math.Pi/2 + float64(k)*math.Pi/float64(n)
			rr := r
			if k%2 == 1 {
				rr = r * inner
			}
			pts = append(pts, [2]float64{math.Cos(a) * rr, math.Sin(a) * rr})
		}
	}
	switch shape {
	case 0: // four-point sparkle
		spikes(4, 0.2)
	case 1: // five-point star
		spikes(5, 0.45)
	case 2: // diamond
		pts = [][2]float64{{0, -r}, {r * 0.62, 0}, {0, r}, {-r * 0.62, 0}}
	case 5: // heart
		for k := 0; k < 28; k++ {
			t := float64(k) / 28 * 2 * math.Pi
			x := 16 * math.Pow(math.Sin(t), 3)
			y := -(13*math.Cos(t) - 5*math.Cos(2*t) - 2*math.Cos(3*t) - math.Cos(4*t))
			pts = append(pts, [2]float64{x / 17 * r, (y/17*0.92 + 0.08) * r})
		}
	}
	return pts
}

// shapeFor: the symbol of star i (Mixed cycles through the others).
func shapeFor(setting, i int) int {
	if setting < 0 || setting >= len(starShapeNames)-1 {
		return []int{0, 1, 2, 3, 4, 5}[(i*7+3)%6]
	}
	return setting
}

/* ---------- Bouncing ball ---------- */

// ballHop: where the ball is between two words (t 0..1 of the way): a parabola that peaks at h.
func ballHop(x0, y0, x1, y1, t, h float64) (float64, float64) {
	t = math.Max(0, math.Min(1, t))
	return x0 + (x1-x0)*t, y0 + (y1-y0)*t - 4*h*t*(1-t)
}

// visualLead: seconds a style shows a word ahead of its time. Entrances (fly in, pop, rise…)
// need a moment before the word can be read; plain highlights only the frame's own delay.
func visualLead(style int) float64 {
	switch style {
	case stRise, stPop, stWordReel, stBounce, stSlide, stScatter, stSwing, stStack, stConstellation, stBouncing, stEcho, stDecode, stGlitch, stInk, stRain:
		return 0.16
	}
	return 0.05
}

// typedChars: how many letters of the line (words joined by spaces) are typed at p — each word
// is typed over its own time, so the typing keeps pace with the singing.
func typedChars(L *Line, ws []string, p float64) int {
	n := 0
	for i, w := range ws {
		r := len([]rune(w))
		if i > 0 {
			n++ // the space before it
		}
		t := L.Words[i].T
		end := t + 0.35
		if i+1 < len(L.Words) {
			end = L.Words[i+1].T
		}
		dur := math.Max(0.08, math.Min(end-t, 0.12*float64(r)+0.1))
		switch {
		case p >= t+dur:
			n += r
		case p > t:
			n += int(math.Ceil(float64(r) * (p - t) / dur))
			return n
		default:
			if i > 0 {
				n-- // the space only comes with the word
			}
			return max(0, n)
		}
	}
	return n
}

// Real constellations (shapes simplified, star positions 0..1). Every one has 6..11 lines, so
// a lyric line never turns into a tangle.
type constellation struct {
	Name  string
	Stars [][2]float64
	Edges [][2]int
}

var constellations = []constellation{
	{"Ursa Major", [][2]float64{{0, .42}, {.17, .32}, {.31, .34}, {.45, .4}, {.5, .72}, {.82, .7}, {.86, .34}},
		[][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}, {4, 5}, {5, 6}, {6, 3}}},
	{"Orion", [][2]float64{{.5, 0}, {.22, .16}, {.76, .2}, {.38, .52}, {.5, .49}, {.62, .46}, {.28, .92}, {.8, .88}},
		[][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 5}, {3, 4}, {4, 5}, {3, 6}, {5, 7}}},
	{"Cygnus", [][2]float64{{.5, 0}, {.5, .38}, {.5, .66}, {.5, 1}, {.22, .46}, {0, .64}, {.78, .3}, {1, .16}},
		[][2]int{{0, 1}, {1, 2}, {2, 3}, {1, 4}, {4, 5}, {1, 6}, {6, 7}}},
	{"Leo", [][2]float64{{.08, .82}, {.12, .58}, {.22, .42}, {.16, .24}, {.3, .1}, {.4, .2}, {.7, .38}, {1, .6}, {.66, .7}},
		[][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}, {4, 5}, {2, 6}, {6, 7}, {7, 8}, {8, 0}}},
	{"Scorpius", [][2]float64{{0, .08}, {.06, .3}, {.14, .18}, {.28, .36}, {.38, .48}, {.46, .62}, {.5, .78}, {.6, .9}, {.76, .96}, {.9, .88}, {.96, .74}, {.88, .64}},
		[][2]int{{0, 2}, {1, 2}, {2, 3}, {3, 4}, {4, 5}, {5, 6}, {6, 7}, {7, 8}, {8, 9}, {9, 10}, {10, 11}}},
	{"Lyra", [][2]float64{{.3, 0}, {.56, .06}, {.46, .34}, {.74, .42}, {.64, .94}, {.36, .86}},
		[][2]int{{0, 1}, {0, 2}, {1, 2}, {2, 5}, {2, 3}, {3, 4}, {4, 5}}},
	{"Gemini", [][2]float64{{.3, 0}, {.58, .06}, {.24, .34}, {.18, .62}, {.12, .96}, {.56, .4}, {.62, .68}, {.72, 1}, {0, .46}, {.86, .56}},
		[][2]int{{0, 1}, {0, 2}, {2, 3}, {3, 4}, {1, 5}, {5, 6}, {6, 7}, {2, 8}, {6, 9}}},
	{"Ursa Minor", [][2]float64{{0, .2}, {.22, .3}, {.4, .42}, {.54, .58}, {.84, .5}, {.92, .8}, {.62, .86}},
		[][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}, {4, 5}, {5, 6}, {6, 3}}},
	{"Pegasus", [][2]float64{{.3, .3}, {.64, .28}, {.66, .66}, {.28, .64}, {.86, .78}, {1, .7}, {.86, .2}, {1, .06}},
		[][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 0}, {2, 4}, {4, 5}, {1, 6}, {6, 7}}},
	{"Draco", [][2]float64{{0, .22}, {.1, .08}, {.2, .2}, {.1, .34}, {.32, .38}, {.44, .58}, {.6, .62}, {.74, .46}, {.84, .3}, {.94, .42}, {1, .64}},
		[][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 0}, {2, 4}, {4, 5}, {5, 6}, {6, 7}, {7, 8}, {8, 9}, {9, 10}}},
	{"Sagittarius", [][2]float64{{.06, .62}, {.3, .34}, {.52, .2}, {.56, .5}, {.78, .42}, {.96, .6}, {.72, .82}, {.38, .8}},
		[][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 1}, {3, 4}, {4, 5}, {5, 6}, {6, 7}, {7, 0}, {7, 3}}},
	{"Corona Borealis", [][2]float64{{0, .3}, {.12, .7}, {.32, .94}, {.52, 1}, {.72, .9}, {.88, .64}, {1, .3}},
		[][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}, {4, 5}, {5, 6}}},
	{"Aquila", [][2]float64{{.5, .42}, {.38, .32}, {.62, .5}, {.12, .14}, {.9, .08}, {.42, .76}, {.56, 1}},
		[][2]int{{1, 0}, {0, 2}, {1, 3}, {2, 4}, {0, 5}, {5, 6}}},
	{"Taurus", [][2]float64{{.42, .52}, {.3, .6}, {.2, .72}, {.48, .62}, {.56, .7}, {.92, .16}, {1, .46}, {0, .94}},
		[][2]int{{0, 3}, {3, 1}, {1, 2}, {3, 4}, {0, 5}, {4, 6}, {2, 7}}},
	{"Cassiopeia & Cepheus", [][2]float64{{0, .3}, {.16, .62}, {.32, .4}, {.48, .7}, {.62, .34}, {.78, .1}, {.96, .22}, {.92, .52}, {.76, .5}},
		[][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}, {4, 5}, {5, 6}, {6, 7}, {7, 8}, {8, 5}}},
	{"Crux & Centaurus", [][2]float64{{.2, 0}, {.22, .4}, {.06, .2}, {.36, .22}, {.5, .5}, {.72, .62}, {.94, .5}, {.86, .9}},
		[][2]int{{0, 1}, {2, 3}, {1, 4}, {3, 4}, {4, 5}, {5, 6}, {5, 7}}},
}

// constellationFor picks the sky for a lyric line: a shape about as big as the line (its stars
// carry the words), chosen per line so songs show many different ones.
func constellationFor(line, words int) int {
	target := words
	if target < 6 {
		target = 6
	}
	if target > 12 {
		target = 12
	}
	var best []int
	bestD := 99
	for i, c := range constellations {
		d := len(c.Stars) - target
		if d < 0 {
			d = -d * 2 // fewer stars than words is worse (words share stars)
		}
		if d < bestD {
			best, bestD = []int{i}, d
		} else if d <= bestD+1 { // a near fit is fine too: more variety
			best = append(best, i)
		}
	}
	return best[int(hash32(uint32(line)*2654435761+uint32(words))%uint32(len(best)))]
}

// skyOrder: the order in which the stars light up — a walk along the lines from star 0,
// so every new star joins one that is already lit.
func skyOrder(c constellation) []int {
	adj := make([][]int, len(c.Stars))
	for _, e := range c.Edges {
		adj[e[0]] = append(adj[e[0]], e[1])
		adj[e[1]] = append(adj[e[1]], e[0])
	}
	seen := make([]bool, len(c.Stars))
	var order []int
	var walk func(int)
	walk = func(i int) {
		seen[i] = true
		order = append(order, i)
		for _, j := range adj[i] {
			if !seen[j] {
				walk(j)
			}
		}
	}
	walk(0)
	for i := range seen { // (every shape is connected, but never lose a star)
		if !seen[i] {
			walk(i)
		}
	}
	return order
}

// skyChunks splits n words over k stars in order: chunk i = words [a, b).
func skyChunks(n, k int) [][2]int {
	if k > n {
		k = n
	}
	out := make([][2]int, k)
	for i := 0; i < k; i++ {
		out[i] = [2]int{i * n / k, (i + 1) * n / k}
	}
	return out
}

// scatterPlace puts words of the given sizes (at scale 1: width, height) in a W×H box at random
// spots that never overlap: each word gets a random size and tilt, then the spot — of many tried
// — that touches no other word and lies farthest from them. If the words can't all fit, they are
// made smaller together (never on top of each other). Same seed, same sizes: same answer.
func scatterPlace(sizes [][2]float64, seed int64, W, H float64) []scatterSpot {
	return scatterPlaceAvoid(sizes, seed, W, H, nil)
}

// scatterBoxes: the room each placed word takes (centre x, y, half width, half height) — what
// the next line should keep clear of while this one fades out.
func scatterBoxes(sizes [][2]float64, spots []scatterSpot) [][4]float64 {
	out := make([][4]float64, 0, len(spots))
	for i, sp := range spots {
		if i >= len(sizes) {
			break
		}
		hw, hh := scatterHalf(sizes[i], sp.Scale, sp.Rot)
		out = append(out, [4]float64{sp.X, sp.Y, hw, hh})
	}
	return out
}

func scatterHalf(s [2]float64, sc, rot float64) (float64, float64) {
	th := math.Abs(rot) * math.Pi / 180
	w, h := s[0]*sc, s[1]*sc*0.82 // (a line's height has room above and below the letters)
	return (w*math.Cos(th)+h*math.Sin(th))/2 + s[1]*sc*0.08, (w*math.Sin(th)+h*math.Cos(th))/2 + s[1]*sc*0.06
}

// scatterPlaceAvoid is scatterPlace that also keeps clear of the given boxes (the line before,
// still fading out) — when there is room; if not, the words come first.
func scatterPlaceAvoid(sizes [][2]float64, seed int64, W, H float64, avoid [][4]float64) []scatterSpot {
	n := len(sizes)
	if n == 0 {
		return nil
	}
	if !(W >= 1) {
		W = 1
	}
	if !(H >= 1) {
		H = 1
	}
	r := rand.New(rand.NewSource(seed))
	scale := make([]float64, n)
	rot := make([]float64, n)
	area := 0.0
	for i, s := range sizes {
		scale[i] = 0.62 + r.Float64()*0.55
		rot[i] = (r.Float64() - 0.5) * 14
		area += s[0] * s[1] * scale[i] * scale[i]
	}
	// everything together may cover at most about half the box
	g := math.Min(1, math.Sqrt(0.46*W*H/math.Max(area, 1)))
	g0 := g
	type box struct{ x, y, hw, hh float64 }
	for attempt := 0; ; attempt++ {
		out := make([]scatterSpot, n)
		placed := make([]box, 0, n)
		ok := true
		rr := rand.New(rand.NewSource(seed*31 + int64(attempt)))
		for i, s := range sizes {
			sc := scale[i] * g
			hw, hh := scatterHalf(s, sc, rot[i]) // the box it needs when tilted, plus a little air
			if 2*hw > W {                        // a word wider than the box: just smaller
				k := W / (2 * hw)
				sc, hw, hh = sc*k, hw*k, hh*k
			}
			best, bestD := box{W / 2, H / 2, hw, hh}, -1.0
			for t := 0; t < 90; t++ {
				c := box{hw + rr.Float64()*math.Max(0, W-2*hw), hh + rr.Float64()*math.Max(0, H-2*hh), hw, hh}
				d := math.Inf(1)
				clash := false
				for _, a := range avoid { // the old line, as long as there's room for it
					if attempt < 3 && math.Abs(c.x-a[0]) < c.hw+a[2] && math.Abs(c.y-a[1]) < c.hh+a[3] {
						clash = true
						break
					}
				}
				for _, p := range placed {
					if clash {
						break
					}
					gx := math.Abs(c.x-p.x) - c.hw - p.hw
					gy := math.Abs(c.y-p.y) - c.hh - p.hh
					if gx < 0 && gy < 0 {
						clash = true
						break
					}
					d = math.Min(d, math.Max(gx, gy))
				}
				if clash {
					continue
				}
				if len(placed) == 0 {
					d = -math.Hypot(c.x-W/2, c.y-H/2) // the first word: near the middle
				}
				if d > bestD || bestD == -1 {
					best, bestD = c, d
				}
			}
			if bestD == -1 && len(placed) > 0 {
				ok = false
				break
			}
			placed = append(placed, best)
			out[i] = scatterSpot{X: best.x, Y: best.y, Scale: sc, Rot: rot[i]}
		}
		if ok || attempt >= 12 {
			if !ok { // (can't happen with sane sizes) one per row, all of them in the box
				for i := range out {
					out[i] = scatterSpot{X: W / 2, Y: (float64(i) + 0.5) * H / float64(n), Scale: math.Min(g, H/float64(n)/math.Max(1, sizes[i][1])), Rot: 0}
				}
			}
			return out
		}
		if attempt == 2 { // no room beside the old line: forget it, back to full size
			g = g0
		} else {
			g *= 0.9 // didn't fit: all a little smaller, try again
		}
	}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
