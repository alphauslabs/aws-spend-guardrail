package alerttext

import (
	"regexp"
	"strings"
)

var digitRun = regexp.MustCompile(`[0-9]+`)

// NameAccounts shows each standalone 12-digit account id in text that names
// gives a name for as "<id> (<name>)" (add-account-names-in-alerts, design D2).
// An id inside an ARN or another id (next to a letter, digit, ':', '/', '-',
// '_', '#', or '.' before it, or a letter, digit, ':', or '/' after it) is left
// alone, as is one already followed by " (", so naming twice changes nothing.
// The error line (from WithError) and anything after it are left exactly as
// they are. An account with an empty name stays bare.
func NameAccounts(text string, names map[string]string) string {
	if len(names) == 0 {
		return text
	}
	head, tail := text, ""
	if i := strings.Index(text, "\n"+ErrorPrefix); i >= 0 {
		head, tail = text[:i], text[i:]
	} else if strings.HasPrefix(text, ErrorPrefix) {
		return text
	}
	var b strings.Builder
	last := 0
	for _, m := range digitRun.FindAllStringIndex(head, -1) {
		start, end := m[0], m[1]
		id := head[start:end]
		name := names[id]
		if len(id) != 12 || name == "" || !standalone(head, start, end) {
			continue
		}
		b.WriteString(head[last:end])
		b.WriteString(" (" + name + ")")
		last = end
	}
	if last == 0 {
		return text
	}
	b.WriteString(head[last:])
	return b.String() + tail
}

func standalone(s string, start, end int) bool {
	if start > 0 {
		c := s[start-1]
		if isAlnum(c) || strings.IndexByte(":/-_#.", c) >= 0 {
			return false
		}
	}
	if end < len(s) {
		c := s[end]
		if isAlnum(c) || c == ':' || c == '/' {
			return false
		}
	}
	return !strings.HasPrefix(s[end:], " (")
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
