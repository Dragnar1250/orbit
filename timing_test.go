package main

import (
	"strings"
	"testing"
)

func TestWordTiming(t *testing.T) {
	// a slow line (6 s until the next one): the words use more of that time than before,
	// but don't run into the next line
	ls := prepare([]Line{{T: 10, Text: "I'm sick, and I'm tired too"}, {T: 16, Text: "next"}})
	L := ls[0]
	last := L.Words[len(L.Words)-1].T
	if last-L.T < 2.0 || L.Sing > 6*0.9+1e-9 || last >= 16 {
		t.Fatalf("slow line squeezed or overflowing: last word at +%.2f, sing %.2f", last-L.T, L.Sing)
	}
	// "sick," holds a breath: the gap after it is longer than after a plain short word
	gapAfter := func(i int) float64 { return L.Words[i+1].T - L.Words[i].T }
	if gapAfter(1) <= gapAfter(2) { // "sick," vs "and"
		t.Fatalf("no breath after the comma: %.2f vs %.2f", gapAfter(1), gapAfter(2))
	}
	// a fast rap line: everything inside the short gap
	ls = prepare([]Line{{T: 0, Text: strings.Repeat("rapid words keep coming ", 6)}, {T: 3, Text: "x"}})
	if w := ls[0].Words; w[len(w)-1].T >= 3 || ls[0].Sing > 2.7+1e-9 {
		t.Fatalf("fast line runs past the next: %.2f", w[len(w)-1].T)
	}
	// a line before a long instrumental break doesn't stretch over the break
	ls = prepare([]Line{{T: 0, Text: "short line here"}, {T: 40, Text: "after the solo"}})
	if ls[0].Sing > 4 {
		t.Fatalf("stretched over the break: %.2f", ls[0].Sing)
	}
	// words stay in order and start with the line
	for _, l := range prepare([]Line{{T: 5, Text: "a bb ccc, dddd eeeee"}, {T: 9}}) {
		for i := 1; i < len(l.Words); i++ {
			if l.Words[i].T <= l.Words[i-1].T {
				t.Fatal("words out of order")
			}
		}
		if len(l.Words) > 0 && l.Words[0].T != l.T {
			t.Fatal("first word must start with the line")
		}
	}
	if wordWeight("beautiful") <= wordWeight("the") || wordWeight("yeah,") <= wordWeight("yeah") {
		t.Fatal("syllables / breath not counted")
	}
}

func TestTypedChars(t *testing.T) {
	L := prepare([]Line{{T: 0, Text: "hello there world"}, {T: 4}})[0]
	ws := []string{"hello", "there", "world"}
	full := len("hello there world")
	prev := -1
	for p := -0.5; p < 5; p += 0.01 {
		n := typedChars(&L, ws, p)
		if n < prev || n < 0 || n > full {
			t.Fatalf("typing went back or out of range at %.2f: %d", p, n)
		}
		prev = n
		// never ahead of the singing: a word isn't typed before it starts
		if wi := wordAt(&L, p); wi >= 0 {
			upto := len(strings.Join(ws[:wi+1], " "))
			if n > upto {
				t.Fatalf("typed ahead of word %d at %.2f: %d > %d", wi, p, n, upto)
			}
		} else if n != 0 {
			t.Fatal("typing before the line")
		}
	}
	if prev != full {
		t.Fatalf("never finished: %d of %d", prev, full)
	}
	for s := 0; s < len(styleNames); s++ {
		if l := visualLead(s); l < 0 || l > 0.25 {
			t.Fatalf("lead %v for %s", l, styleNames[s])
		}
	}
}

func TestConstellations(t *testing.T) {
	if len(constellations) < 14 {
		t.Fatal("want many skies")
	}
	for _, c := range constellations {
		if n := len(c.Edges); n < 6 || n > 11 {
			t.Errorf("%s: %d lines (want 6..11)", c.Name, n)
		}
		for _, s := range c.Stars {
			if s[0] < 0 || s[0] > 1 || s[1] < 0 || s[1] > 1 {
				t.Errorf("%s: star outside the box", c.Name)
			}
		}
		for _, e := range c.Edges {
			if e[0] == e[1] || e[0] < 0 || e[1] < 0 || e[0] >= len(c.Stars) || e[1] >= len(c.Stars) {
				t.Errorf("%s: bad line %v", c.Name, e)
			}
		}
		o := skyOrder(c)
		if len(o) != len(c.Stars) {
			t.Errorf("%s: order misses stars", c.Name)
		}
		// each star after the first touches an earlier one: lines grow, never float
		pos := map[int]int{}
		for i, s := range o {
			pos[s] = i
		}
		for i := 1; i < len(o); i++ {
			ok := false
			for _, e := range c.Edges {
				if (e[0] == o[i] && pos[e[1]] < i) || (e[1] == o[i] && pos[e[0]] < i) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s: star %d lights up unconnected", c.Name, o[i])
			}
		}
		// stars far enough apart to carry a word each
		for i := range c.Stars {
			for j := i + 1; j < len(c.Stars); j++ {
				dx, dy := c.Stars[i][0]-c.Stars[j][0], c.Stars[i][1]-c.Stars[j][1]
				if dx*dx+dy*dy < 0.1*0.1 {
					t.Errorf("%s: stars %d and %d too close", c.Name, i, j)
				}
			}
		}
	}
	used := map[int]bool{}
	for line := 0; line < 200; line++ {
		for words := 1; words < 25; words++ {
			c := constellationFor(line, words)
			used[c] = true
			k := len(constellations[c].Stars)
			ch := skyChunks(words, k)
			if len(ch) != min(words, k) || ch[0][0] != 0 || ch[len(ch)-1][1] != words {
				t.Fatalf("chunks %v for %d words on %d stars", ch, words, k)
			}
			for i := 1; i < len(ch); i++ {
				if ch[i][0] != ch[i-1][1] || ch[i][1] <= ch[i][0] {
					t.Fatalf("chunks not continuous %v", ch)
				}
			}
			if words >= 8 && k < 7 {
				t.Fatalf("%d words on a %d-star sky", words, k)
			}
		}
	}
	if len(used) < 10 {
		t.Fatalf("only %d skies used", len(used))
	}
}
