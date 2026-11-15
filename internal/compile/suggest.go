package compile

import "strings"

// DidYouMean returns " (did you mean X?)" when word is a likely typo of
// one of the options, else "". A typo is within about one edit per three
// letters, where an edit is an insertion, deletion, substitution, or a
// swap of two neighbouring letters (optimal string alignment distance).
func DidYouMean(word string, options []string) string {
	if s := Closest(word, options); s != "" {
		return " (did you mean " + s + "?)"
	}
	return ""
}

func Closest(word string, options []string) string {
	if len([]rune(word)) < 3 {
		return "" // I and J are different names, not a typo
	}
	best, bestD := "", 1<<30
	w := strings.ToLower(word)
	for _, o := range options {
		if o == word {
			return "" // exact match: not a typo
		}
		if d := OSADistance(w, strings.ToLower(o)); d < bestD {
			best, bestD = o, d
		}
	}
	if bestD <= max(1, len([]rune(word))/3) {
		return best
	}
	return ""
}

func OSADistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1) // swapped neighbours
			}
		}
	}
	return d[len(ra)][len(rb)]
}

// Indent prefixes every line of s.
func Indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
