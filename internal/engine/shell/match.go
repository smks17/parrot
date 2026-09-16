package shell

import (
	"path"
	"strings"
)

func (sh *Shell) matchFiles(word Word, expanded []string) []string {
	if !hasPattern(word) {
		return expanded
	}

	var matched []string
	for _, field := range expanded {
		matched = append(matched, sh.glob(field)...)
	}
	return matched
}

// hasPattern reports whether a wildcard was written in the word itself.
func hasPattern(word Word) bool {
	for _, piece := range word {
		if piece.Quote == Bare && hasMeta(piece.Text) {
			return true
		}
	}
	return false
}

func hasMeta(s string) bool { return strings.ContainsAny(s, "*?[") }

// glob returns the file names matching a pattern, or the pattern itself when
// nothing matches, which is what the shell does.
func (sh *Shell) glob(pattern string) []string {
	dir, base := path.Split(pattern) // "notes/*.md" -> "notes/" and "*.md"
	if !hasMeta(base) {
		return []string{pattern}
	}

	where := dir
	if where == "" {
		where = "."
	}
	entries, err := sh.fs.List(where)
	if err != nil {
		return []string{pattern}
	}

	var matches []string
	for _, entry := range entries {
		// A name starting with a dot is only matched by a pattern that
		// starts with one too.
		if strings.HasPrefix(entry.Name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		if match(base, entry.Name) {
			matches = append(matches, dir+entry.Name)
		}
	}
	if len(matches) == 0 {
		return []string{pattern}
	}
	return matches
}

// match reports whether a name matches a pattern. path.Match already knows the
// rules — * for any run of characters, ? for one, [abc] and [a-z] for a set —
// so the only difference to iron out is that a shell negates a set with
// [!abc] where path.Match writes [^abc].
//
// A malformed pattern, such as an unclosed [, matches nothing; glob then hands
// back the word as it was written, which is what should happen anyway.
func match(pattern, name string) bool {
	matched, err := path.Match(strings.ReplaceAll(pattern, "[!", "[^"), name)
	return matched && err == nil
}
