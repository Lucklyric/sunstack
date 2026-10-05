package core

import (
	"regexp"
	"strings"
)

// Task briefs (design §16.1): a message of type task carries labeled lines,
// so the receiver knows the outcome, its limits and how to prove it without
// asking. A brief with a missing required label is refused at the door.

// BriefLabels are the labels every task brief needs.
var BriefLabels = []string{"Goal", "Scope", "Done when", "Verify", "Report"}

// briefLabelRe matches any brief label, required or optional, at a line start.
var briefLabelRe = regexp.MustCompile(`(?i)^(goal|scope|done when|verify|report|context|timebox|not):(.*)$`)

// MissingBrief lists the required labels a brief lacks. A label counts only
// with text after it, on its own line or on the indented lines below it.
func MissingBrief(body string) []string {
	have := map[string]bool{}
	cur := ""
	for _, l := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if m := briefLabelRe.FindStringSubmatch(strings.TrimRight(l, " \t")); m != nil {
			cur = strings.ToLower(m[1])
			if strings.TrimSpace(m[2]) != "" {
				have[cur] = true
			}
			continue
		}
		if cur != "" && (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && strings.TrimSpace(l) != "" {
			have[cur] = true
			continue
		}
		cur = ""
	}
	var out []string
	for _, l := range BriefLabels {
		if !have[strings.ToLower(l)] {
			out = append(out, l)
		}
	}
	return out
}

// CheckBrief refuses a task brief with a missing required label.
func CheckBrief(body string) error {
	if miss := MissingBrief(body); len(miss) > 0 {
		return fail(ExitUsage, "missing_brief", "a task brief needs %s; missing: %s", strings.Join(BriefLabels, ", "), strings.Join(miss, ", "))
	}
	return nil
}
