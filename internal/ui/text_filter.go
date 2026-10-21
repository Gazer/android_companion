package ui

import "strings"

// textFilter is the parsed text of the log filter field.
//
// Terms are separated by spaces and must all match: a line is shown if it
// contains every plain term and none of the "~" terms. Matching ignores
// case. For example:
//
//	error ~Choreographer       lines with "error", but not "Choreographer"
//	"player started" ~debug    a phrase with spaces, and an exclusion
//	\~home                     a literal "~home"
type textFilter struct {
	include []string // lower case
	exclude []string // lower case
}

// match reports whether line passes the filter.
func (f textFilter) match(line string) bool {
	for _, t := range f.exclude {
		if containsFold(line, t) {
			return false
		}
	}
	for _, t := range f.include {
		if !containsFold(line, t) {
			return false
		}
	}
	return true
}

// parseTextFilter splits the filter field's text into terms. A term
// prefixed with "~" excludes; double quotes group words into a single
// term; "\~" starts a term with a literal "~". Empty terms (a lone "~"
// while typing) are ignored.
func parseTextFilter(s string) textFilter {
	var f textFilter
	for {
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			return f
		}

		exclude := false
		switch {
		case strings.HasPrefix(s, `\~`):
			s = s[1:] // keep the "~" as part of the term
		case s[0] == '~':
			exclude = true
			s = s[1:]
		}

		var term string
		if strings.HasPrefix(s, `"`) {
			if end := strings.IndexByte(s[1:], '"'); end >= 0 {
				term, s = s[1:1+end], s[2+end:]
			} else {
				term, s = s[1:], "" // unclosed quote: up to the end
			}
		} else if end := strings.IndexAny(s, " \t"); end >= 0 {
			term, s = s[:end], s[end:]
		} else {
			term, s = s, ""
		}

		if term == "" {
			continue
		}
		term = strings.ToLower(term)
		if exclude {
			f.exclude = append(f.exclude, term)
		} else {
			f.include = append(f.include, term)
		}
	}
}
