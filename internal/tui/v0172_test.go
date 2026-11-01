package tui

import "testing"

// §34.15: leaving Hosts outside a team returns to Org.
func TestHostsBackOutsideTeam(t *testing.T) {
	m := hostsModel(t, 100)
	if m.p != nil {
		t.Fatal("expected a host-only model")
	}
	m.Update(key("h"))
	if m.view != viewHosts {
		t.Fatal("h did not open Hosts")
	}
	m.Update(key("h"))
	if m.view != viewOrg {
		t.Errorf("h from Hosts outside a team went to view %d, not Org", m.view)
	}
}
