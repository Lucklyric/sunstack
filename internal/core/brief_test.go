package core

import (
	"reflect"
	"testing"
)

func TestMissingBriefBlocks(t *testing.T) {
	head := "Goal: fix it\nScope: core\nVerify: go test\nReport: summary\n"
	for body, want := range map[string][]string{
		head + "Done when: tests pass\n":                    nil,
		head + "Done when:\n- tests pass\n- docs updated\n": nil,
		head + "Done when:\n\n1. tests pass\n":              nil,
		head + "Done when:\n  tests pass\n":                 nil,
		head + "Done when:\nnot a list item\n":              {"Done when"},
		head + "Done when:\n":                               {"Done when"},
		"Goal:\n- a\nScope: s\nDone when: d\nVerify: v\n":   {"Report"},
	} {
		if got := MissingBrief(body); !reflect.DeepEqual(got, want) {
			t.Errorf("MissingBrief(%q) = %v, want %v", body, got, want)
		}
	}
}
