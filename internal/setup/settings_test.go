package setup

import (
	"strings"
	"testing"
)

func TestEditAllowKeepsOrderAndOtherKeys(t *testing.T) {
	src := []byte(`{
  "model": "opus",
  "permissions": {
    "deny": ["Bash(rm -rf *)"],
    "allow": ["Read"]
  },
  "hooks": {"Stop": []}
}`)
	out, changed, err := editRule(src, "allow", AllowRule, true)
	if err != nil || !changed {
		t.Fatalf("add: changed=%v err=%v", changed, err)
	}
	s := string(out)
	for _, want := range []string{`"Read"`, `"Bash(sunstack *)"`, `"Bash(rm -rf *)"`, `"hooks"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in:\n%s", want, s)
		}
	}
	if !(strings.Index(s, `"model"`) < strings.Index(s, `"permissions"`) && strings.Index(s, `"permissions"`) < strings.Index(s, `"hooks"`)) {
		t.Errorf("top-level key order changed:\n%s", s)
	}
	if strings.Index(s, `"deny"`) > strings.Index(s, `"allow"`) {
		t.Errorf("permissions key order changed:\n%s", s)
	}

	again, changed, _ := editRule(out, "allow", AllowRule, true)
	if changed || string(again) != s {
		t.Error("adding twice must be a no-op")
	}

	back, changed, err := editRule(out, "allow", AllowRule, false)
	if err != nil || !changed || strings.Contains(string(back), "sunstack") || !strings.Contains(string(back), `"Read"`) {
		t.Errorf("remove: changed=%v err=%v\n%s", changed, err, back)
	}
}

func TestEditAllowEmptyAndMissing(t *testing.T) {
	for _, src := range []string{"", "{}", `{"permissions": {}}`} {
		out, changed, err := editRule([]byte(src), "allow", AllowRule, true)
		if err != nil || !changed || !strings.Contains(string(out), `"Bash(sunstack *)"`) {
			t.Errorf("%q: changed=%v err=%v out=%s", src, changed, err, out)
		}
	}
	if _, _, err := editRule([]byte(`[1,2]`), "allow", AllowRule, true); err == nil {
		t.Error("a non-object settings file must be an error, not overwritten")
	}
}

func TestAllowAndAskTogether(t *testing.T) {
	out, _, err := editRule([]byte(`{"permissions":{"allow":["Read"]}}`), "allow", AllowRule, true)
	if err == nil {
		out, _, err = editRule(out, "ask", AskRule, true)
	}
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"ask": [`) || !strings.Contains(s, `"Bash(sunstack amend *)"`) || !strings.Contains(s, `"Bash(sunstack *)"`) {
		t.Errorf("want both rules:\n%s", s)
	}
}
