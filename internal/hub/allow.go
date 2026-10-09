package hub

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// Grants (§20.7): which hosts may ask this one to spawn, peek or update.
// The receiving host decides; the hub cannot grant. A grant names the host
// by its ID and the fingerprint of the key it had when granted, so a
// renamed host or a changed key never inherits it.

// GrantKinds are the requests a grant can allow.
var GrantKinds = []string{"peek", "spawn", "update"}

// Grant is one host's grant. User grants are keyed by host ID in allow.json;
// each agent grant has its own <host ID>:<kind>:agents entry.
type Grant struct {
	OrgID       string   `json:"org_id"`
	HostID      string   `json:"host_id"`
	Name        string   `json:"name"` // as when granted, for the listing
	Fingerprint string   `json:"fingerprint"`
	Kinds       []string `json:"kinds"`
	At          string   `json:"at"`
	Teams       []string `json:"teams,omitempty"` // agent grants: resolved local team IDs
	Max         int      `json:"max,omitempty"`
}

func allowPath() string { return filepath.Join(remoteDir(), "allow.json") }

func lockGrants() (func(), error) {
	return core.LockDir(filepath.Join(remoteDir(), "allow.lock"), "host grants")
}

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
	pins := loadPins()
	byMachine := ""
	if r := cachedHosts(); r != nil {
		if h := r.byName(name); h != nil {
			byMachine = h.ID
		}
	}
	for id, p := range pins {
		if strings.EqualFold(p.Name, name) || id == byMachine {
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
	unlock, err := lockGrants()
	if err != nil {
		return nil, err
	}
	defer unlock()
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
	return deny(name, kinds, false)
}

func deny(name string, kinds []string, agents bool) error {
	unlock, err := lockGrants()
	if err != nil {
		return err
	}
	defer unlock()
	gs := loadGrants()
	pins := loadPins()
	for key, g := range gs {
		isAgent := key != g.HostID
		if isAgent != agents || (agents && !slices.Contains(g.Kinds, kinds[0])) {
			continue
		}
		// The host's name now; the name at grant time only for a host
		// no longer pinned.
		current := g.Name
		if p := pins[g.HostID]; p != nil {
			current = p.Name
		}
		if strings.EqualFold(current, name) {
			g.Kinds = slices.DeleteFunc(g.Kinds, func(k string) bool { return slices.Contains(kinds, k) })
			if len(g.Kinds) == 0 {
				delete(gs, key)
			}
			return writeJSON(allowPath(), gs, 0o600)
		}
	}
	return failErr("not_found", "%s has no grant here", name)
}

// Allowed says whether the host with this ID may ask for kind: granted in
// this org, with the key it has now.
func Allowed(hostID, kind string) bool {
	if !slices.Contains(GrantKinds, kind) {
		return false
	}
	g := loadGrants()[hostID]
	return validGrant(hostID, g) && slices.Contains(g.Kinds, kind)
}

func validGrant(hostID string, g *Grant) bool {
	p := loadPins()[hostID]
	return g != nil && g.HostID == hostID && p != nil && p.Changed == nil && g.OrgID == orgID() &&
		g.Fingerprint == p.Keys.Fingerprint()
}

func agentKind(kind string) (string, error) {
	if kind != "spawn" && kind != "peek" {
		return "", failErr("usage", "only spawn and peek can be granted to agents")
	}
	return kind + ":agents", nil
}

// AllowAgents replaces one agent grant, separate from the user's grants.
// Team names are resolved once to IDs; nil teams for peek copies the current
// valid spawn agent grant's teams. max is a concurrency cap, not a budget.
func AllowAgents(name, kind string, teams []string, max int) (*Grant, error) {
	k, err := agentKind(kind)
	if err != nil {
		return nil, err
	}
	if max < 1 || max > 10 {
		return nil, failErr("usage", "--max must be 1 to 10")
	}
	var ids []string
	for _, name := range teams {
		if strings.TrimSpace(name) == "" {
			return nil, failErr("usage", "--teams needs local team names")
		}
		p, err := core.ResolveTeam(name)
		if err != nil {
			return nil, err
		}
		t, ok := p.Team()
		if !ok {
			return nil, failErr("usage", "%s has no valid TEAM file; migrate the team first", name)
		}
		if !slices.Contains(ids, t.ID) {
			ids = append(ids, t.ID)
		}
	}
	unlock, err := lockGrants()
	if err != nil {
		return nil, err
	}
	defer unlock()
	id, p, err := pinByName(name)
	if err != nil {
		return nil, err
	}
	gs := loadGrants()
	if kind == "peek" && teams == nil {
		spawn := gs[id+":spawn:agents"]
		if !validGrant(id, spawn) || !slices.Contains(spawn.Kinds, "spawn:agents") {
			return nil, failErr("usage", "peek --agents needs --teams or a spawn --agents grant")
		}
		ids = slices.Clone(spawn.Teams)
	}
	if len(ids) == 0 {
		return nil, failErr("usage", "--teams needs at least one local team")
	}
	sort.Strings(ids)
	g := &Grant{OrgID: orgID(), HostID: id, Name: p.Name, Fingerprint: p.Keys.Fingerprint(),
		Kinds: []string{k}, At: stamp(time.Now()), Teams: ids, Max: max}
	gs[id+":"+k] = g
	return g, writeJSON(allowPath(), gs, 0o600)
}

// DenyAgents removes only this kind's agent grant, leaving other grants.
func DenyAgents(name, kind string) error {
	k, err := agentKind(kind)
	if err != nil {
		return err
	}
	return deny(name, []string{k}, true)
}

// AgentAllowed checks only an agent grant for kind (spawn or peek), bound
// to this org, the host's current key, and the resolved local team ID.
// The returned max applies across all teams in that grant.
func AgentAllowed(hostID, kind, teamID string) (max int, ok bool) {
	k, err := agentKind(kind)
	if err != nil {
		return 0, false
	}
	g := loadGrants()[hostID+":"+k]
	if !validGrant(hostID, g) || !slices.Contains(g.Kinds, k) || !slices.Contains(g.Teams, teamID) || g.Max < 1 || g.Max > 10 {
		return 0, false
	}
	return g.Max, true
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
		if p := loadPins()[g.HostID]; p != nil {
			name = p.Name
			if p.Changed != nil || p.Keys.Fingerprint() != g.Fingerprint {
				state = "  (void: its key changed since; grant again after sunstack org trust)"
			}
		}
		if len(g.Teams) > 0 {
			fmt.Fprintf(&b, "  %s: %s, teams %s, max %d, key %s, age %s%s\n", name, strings.Join(g.Kinds, ", "), strings.Join(g.Teams, ", "), g.Max, g.Fingerprint, core.SSHAge(g.At), state)
		} else {
			fmt.Fprintf(&b, "  %s: %s, key %s%s\n", name, strings.Join(g.Kinds, ", "), g.Fingerprint, state)
		}
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
		case "spawn:agents":
			b.WriteString("  spawn:agents: agents of that host may start Claude or Codex sessions as you, in these teams, under this host's permission rules\n  repeated jobs keep costing; max limits concurrency, not a budget\n")
		case "peek:agents":
			b.WriteString("  peek:agents: shows every pane in these teams, your own sessions included; pane text may include secrets\n")
		}
	}
	return b.String()
}
