package qr

import "github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators/internal/qrcore"

// bestMask returns the mask with the lowest penalty (ISO/IEC 18004 7.8.3.1),
// preferring the lowest mask number on ties. Masks are scored before format
// and version information is drawn, so those areas count as light.
func bestMask(m *qrcore.Matrix) int {
	best, bestScore := 0, -1
	for mask, pattern := range qrcore.MaskPatterns {
		candidate := m.Clone()
		candidate.ApplyMask(pattern)
		if score := penalty(candidate); bestScore < 0 || score < bestScore {
			best, bestScore = mask, score
		}
	}
	return best
}

// penalty scores a masked symbol by the four rules of ISO/IEC 18004 Table 11.
func penalty(m *qrcore.Matrix) int {
	size := m.Size
	score := 0
	dark := 0
	row := make([]bool, size)
	column := make([]bool, size)

	for i := 0; i < size; i++ {
		for j := 0; j < size; j++ {
			row[j] = m.Dark(j, i)
			column[j] = m.Dark(i, j)
			if row[j] {
				dark++
			}
			// Rule 2: each 2×2 block of one color.
			if i > 0 && j > 0 && row[j] == m.Dark(j-1, i) && row[j] == m.Dark(j, i-1) && row[j] == m.Dark(j-1, i-1) {
				score += 3
			}
		}
		score += runPenalty(row) + runPenalty(column)
		score += finderLikePenalty(row) + finderLikePenalty(column)
	}

	// Rule 4: 10 points per full 5% the dark proportion strays from 50%.
	total := size * size
	deviation := 20*dark - 10*total
	if deviation < 0 {
		deviation = -deviation
	}
	score += deviation / total * 10
	return score
}

// runPenalty implements rule 1: 3 points for each run of five same-colored
// modules, plus one per module beyond five.
func runPenalty(line []bool) int {
	score := 0
	run := 1
	for i := 1; i <= len(line); i++ {
		if i < len(line) && line[i] == line[i-1] {
			run++
			continue
		}
		if run >= 5 {
			score += run - 2
		}
		run = 1
	}
	return score
}

var finderLike = []bool{true, false, true, true, true, false, true}

// finderLikePenalty implements rule 3: 40 points for each dark-light-dark-
// dark-dark-light-dark pattern with four light modules on either side. The
// symbol edge counts as light, since the quiet zone lies beyond it.
func finderLikePenalty(line []bool) int {
	score := 0
	n := len(line)
	for idx := indexOf(line, 0); idx >= 0; {
		next := idx + 7
		if allLight(line[max(idx-4, 0):idx]) || allLight(line[next:min(next+4, n)]) {
			score += 40
		} else {
			// The pattern's third module may start the next match.
			next = idx + 4
		}
		idx = indexOf(line, next)
	}
	return score
}

func indexOf(line []bool, from int) int {
	for i := from; i+len(finderLike) <= len(line); i++ {
		match := true
		for k, want := range finderLike {
			if line[i+k] != want {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func allLight(modules []bool) bool {
	for _, dark := range modules {
		if dark {
			return false
		}
	}
	return true
}
