package main

// 1.19: the maths of the two "board" lyric styles — Split-flap and LED board — kept free of
// Windows calls so it can be unit-tested.

import "math"

const (
	flapStagger = 0.028 // seconds between neighbouring letters starting to flip
	flapStep    = 0.055 // seconds a flap shows each in-between letter
	flapMinTurn = 3     // in-between letters at least …
	flapMaxTurn = 7     // … and at most
)

// flapRows wraps the words into rows of at most maxChars letters (a word longer than a
// row gets a row of its own and is cut by the caller's box).
func flapRows(words []string, maxChars int) [][]int {
	if maxChars < 4 {
		maxChars = 4
	}
	var rows [][]int
	var row []int
	n := 0
	for i, w := range words {
		l := len([]rune(w))
		if len(row) > 0 && n+1+l > maxChars {
			rows = append(rows, row)
			row, n = nil, 0
		}
		if len(row) > 0 {
			n++
		}
		row = append(row, i)
		n += l
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return rows
}

// flapTurns: how many in-between letters tile k shows before its own (the same every time).
func flapTurns(k int) int {
	return flapMinTurn + int(hash32(uint32(k)*2654435761+17)%uint32(flapMaxTurn-flapMinTurn+1))
}

// flapState: for tile k (in reading order) at e seconds into the line — whether it has
// settled, which in-between letter it shows (0..turns-1) and how far through that flip it is.
func flapState(k int, e float64) (settled bool, turn int, phase float64) {
	t := e - float64(k)*flapStagger
	if t < 0 {
		return false, 0, 0
	}
	n := flapTurns(k)
	if t >= float64(n)*flapStep {
		return true, n, 0
	}
	turn = int(t / flapStep)
	return false, turn, math.Mod(t, flapStep) / flapStep
}

// flapSettle: seconds until every tile of the line has settled.
func flapSettle(words []string) float64 {
	k := 0
	for _, w := range words {
		k += len([]rune(w)) + 1
	}
	return float64(k)*flapStagger + float64(flapMaxTurn)*flapStep
}

// flapGlyph: the in-between letter tile k shows on its turn-th flip.
func flapGlyph(k, turn int, final rune) rune {
	const board = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	if final == ' ' {
		return ' '
	}
	return rune(board[hash32(uint32(k)*7919+uint32(turn)*104729+uint32(final))%uint32(len(board))])
}

// ledLine joins per-word dot columns into one line with a 3-column space between words.
// It returns the columns and where each word starts and ends.
func ledLine(wordCols [][]uint16) (cols []uint16, start, end []int) {
	for i, wc := range wordCols {
		if i > 0 {
			cols = append(cols, 0, 0, 0)
		}
		start = append(start, len(cols))
		cols = append(cols, wc...)
		end = append(end, len(cols))
	}
	return
}

// ledScroll: the panel's scroll (in columns) so the sung point sits at `anchor` of a panel
// `panel` columns wide. Before the first word the text slides in from the right edge.
func ledScroll(start, end []int, wi int, frac, intro float64, panel int) float64 {
	anchor := float64(panel) * 0.42
	var u float64
	switch {
	case len(start) == 0:
		return 0
	case wi < 0:
		u = -anchor * (1 - math.Max(0, math.Min(1, intro)))
	case wi >= len(start):
		u = float64(end[len(end)-1])
	default:
		u = float64(start[wi]) + math.Max(0, math.Min(1, frac))*float64(end[wi]-start[wi])
	}
	return anchor - u
}

// downsampleGlyph turns a big rendered letter (alpha per pixel, w×h, h = rows*k) into dot
// columns: a dot is lit when enough of its k×k block is covered. Empty columns at both sides
// are trimmed (a letter keeps at least one column).
func downsampleGlyph(alpha []uint8, w, h, rows int) []uint16 {
	if rows <= 0 || rows > 16 || w <= 0 || h <= 0 {
		return nil
	}
	k := h / rows
	if k < 1 {
		return nil
	}
	n := w / k
	cols := make([]uint16, n)
	for c := 0; c < n; c++ {
		for r := 0; r < rows; r++ {
			sum := 0
			for y := r * k; y < (r+1)*k; y++ {
				for x := c * k; x < (c+1)*k; x++ {
					sum += int(alpha[y*w+x])
				}
			}
			if sum > k*k*255*36/100 {
				cols[c] |= 1 << uint(r)
			}
		}
	}
	lo, hi := 0, len(cols)
	for lo < hi && cols[lo] == 0 {
		lo++
	}
	for hi > lo && cols[hi-1] == 0 {
		hi--
	}
	if lo == hi {
		return []uint16{0}
	}
	return cols[lo:hi]
}
