package hub

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// Grants (§20.7): which hosts may ask this one to spawn, peek or update.
// The receiving host decides; the hub cannot grant. A grant names the host
// by its ID and the fingerprint of the key it had when granted, so a
// renamed host or a changed key never inherits it.

// GrantKinds are the requests a grant can allow.
var GrantKinds = []string{"peek", "spawn", "update"}

// Grant is one host's grant.
type Grant struct {
	OrgID       string   `json:"org_id"`
	HostID      string   `json:"host_id"`
	Name        string   `json:"name"` // as when granted, for the listing
	Fingerprint string   `json:"fingerprint"`
	Kinds       []string `json:"kinds"`
	At          string   `json:"at"`
}

func allowPath() string { return filepath.Join(remoteDir(), "allow.json") }

func loadGrants() map[string]*Grant {
	gs := map[string]*Grant{}
	_ = readJSON(allowPath(), &gs)
	return gs
}

func orgID() string {
	if c, err := LoadConfig(); err == nil && c != nil {
		return c.OrgID
	}
	return ""
}

func checkKinds(kinds []string) error {
	if len(kinds) == 0 {
		return failErr("usage", "name what to allow: %s", strings.Join(GrantKinds, ", "))
	}
	for _, k := range kinds {
		if !slices.Contains(GrantKinds, k) {
			return failErr("usage", "%s cannot be granted; only %s", k, strings.Join(GrantKinds, ", "))
		}
	}
	return nil
}

// pinByName finds a trusted host by name, now.
func pinByName(name string) (string, *Pin, error) {
	for id, p := range loadPins() {
		if strings.EqualFold(p.Name, name) {
			if p.Changed != nil {
				return "", nil, failErr("key_changed", "%s's key changed; compare fingerprints and run sunstack org trust %s first", p.Name, p.Name)
			}
			return id, p, nil
		}
	}
	return "", nil, failErr("not_found", "no host %s in the org (sunstack org keys lists them)", name)
}

// Allow grants kinds to the host called name, resolved once, now.
func Allow(name string, kinds []string) (*Grant, error) {
	if err := checkKinds(kinds); err != nil {
		return nil, err
	}
	id, p, err := pinByName(name)
	if err != nil {
		return nil, err
	}
	gs := loadGrants()
	g := gs[id]
	fp := p.Keys.Fingerprint()
	if g == nil || g.Fingerprint != fp || g.OrgID != orgID() {
		g = &Grant{OrgID: orgID(), HostID: id, Fingerprint: fp}
	}
	g.Name, g.At = p.Name, time.Now().UTC().Format(time.RFC3339)
	for _, k := range kinds {
		if !slices.Contains(g.Kinds, k) {
			g.Kinds = append(g.Kinds, k)
		}
	}
	sort.Strings(g.Kinds)
	gs[id] = g
	return g, writeJSON(allowPath(), gs, 0o600)
}

// Deny removes kinds from the host called name; its grant goes once empty.
func Deny(name string, kinds []string) error {
	if err := checkKinds(kinds); err != nil {
		return err
	}
	gs := loadGrants()
	pins := loadPins()
	for id, g := range gs {
		// The host's name now; the name at grant time only for a host
		// no longer pinned.
		current := g.Name
		if p := pins[id]; p != nil {
			current = p.Name
		}
		if strings.EqualFold(current, name) {
			g.Kinds = slices.DeleteFunc(g.Kinds, func(k string) bool { return slices.Contains(kinds, k) })
			if len(g.Kinds) == 0 {
				delete(gs, id)
			}
			return writeJSON(allowPath(), gs, 0o600)
		}
	}
	return failErr("not_found", "%s has no grant here", name)
}

// Allowed says whether the host with this ID may ask for kind: granted in
// this org, with the key it has now.
func Allowed(hostID, kind string) bool {
	g := loadGrants()[hostID]
	p := loadPins()[hostID]
	return g != nil && p != nil && p.Changed == nil && g.OrgID == orgID() &&
		g.Fingerprint == p.Keys.Fingerprint() && slices.Contains(g.Kinds, kind)
}

// GrantsText lists the grants, for sunstack org allow with no arguments.
func GrantsText() string {
	gs := loadGrants()
	if len(gs) == 0 {
		return "No host may spawn, peek or update here (sunstack org allow <host> peek|spawn|update).\n"
	}
	var b strings.Builder
	b.WriteString("Hosts that may ask this host:\n")
	ids := make([]string, 0, len(gs))
	for id := range gs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		g := gs[id]
		name := g.Name
		state := ""
		if p := loadPins()[id]; p != nil {
			name = p.Name
			if p.Changed != nil || p.Keys.Fingerprint() != g.Fingerprint {
				state = "  (void: its key changed since; grant again after sunstack org trust)"
			}
		}
		fmt.Fprintf(&b, "  %s: %s, key %s%s\n", name, strings.Join(g.Kinds, ", "), g.Fingerprint, state)
	}
	return b.String()
}

// GrantWarning is what each kind exposes, printed when it is granted.
func GrantWarning(kinds []string) string {
	var b strings.Builder
	for _, k := range kinds {
		switch k {
		case "peek":
			b.WriteString("  peek: shows up to 50 lines of a session's pane, which may include secrets printed there\n")
		case "spawn":
			b.WriteString("  spawn: starts Claude or Codex as you on this host, in a team folder\n")
		case "update":
			b.WriteString("  update: replaces sunstack on this host and restarts its connector\n")
		}
	}
	return b.String()
}
