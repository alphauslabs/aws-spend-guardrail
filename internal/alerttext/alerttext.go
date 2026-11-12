// Package alerttext holds the rules for local alert bodies, which a person
// reads as an email, often during an incident (fix-alert-wording): full
// sentences, targets named for people, and any error text set apart on a
// last line starting with "Error: ".
package alerttext

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ErrorPrefix starts the line that carries error detail.
const ErrorPrefix = "Error: "

// WithError appends err's text to body on its own last line, or returns body
// alone when err is nil.
func WithError(body string, err error) string {
	if err == nil {
		return body
	}
	return body + "\n\n" + ErrorPrefix + err.Error()
}

// Check reports how body breaks the rules, or nil. Tests call it on every
// alert they see, so a new alert can't slip into log-line style.
func Check(body string) error {
	var lines []string
	for _, l := range strings.Split(body, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return errors.New("empty alert body")
	}
	prose := lines
	if last := lines[len(lines)-1]; strings.HasPrefix(last, ErrorPrefix) {
		prose = lines[:len(lines)-1]
		if len(prose) == 0 {
			return errors.New("an error line alone, with no sentence before it")
		}
	}
	for i, l := range prose {
		if strings.HasPrefix(l, ErrorPrefix) {
			return fmt.Errorf("line %d: an Error: line must be the last line", i+1)
		}
		if strings.HasPrefix(l, "account:") || strings.HasPrefix(l, "ou:") {
			return fmt.Errorf("line %d starts with a raw target id: %q", i+1, l)
		}
	}
	if r := []rune(prose[0]); !unicode.IsUpper(r[0]) {
		return fmt.Errorf("does not start with a capital letter: %q", prose[0])
	}
	// The sentences end with a period. A list that follows them (one item per
	// line, as in the unprotected-accounts report) may end without one, but
	// the sentence introducing it must end in "." or ":".
	end := len(prose) - 1
	for end > 0 && isListItem(prose[end]) {
		end--
	}
	last := strings.TrimSpace(prose[end])
	if end < len(prose)-1 {
		if !strings.HasSuffix(last, ".") && !strings.HasSuffix(last, ":") {
			return fmt.Errorf("the sentence before the list does not end with a period or colon: %q", last)
		}
		return nil
	}
	if !strings.HasSuffix(last, ".") {
		return fmt.Errorf("does not end with a period: %q", last)
	}
	return nil
}

// isListItem is a line in a list after the sentences: "- ..." or an indented
// line.
func isListItem(l string) bool {
	return strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "  ")
}
