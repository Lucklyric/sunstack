package hub

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// HostView is one host of the org as this host sees it (§18.6, §18.7).
type HostView struct {
	ID, Name    string
	Hub, You    bool
	CanSend     bool
	Waiting     int           // mail waiting for it on the hub
	State       string        // live, stale, offline, not synced yet; the own host is live
	ContactAge  time.Duration // since the hub last heard from it
	SnapAge     time.Duration // since its state last changed
	HasSnap     bool
	Org         *core.Org // its last snapshot; nil for this host and a host never synced
	Fingerprint string    // of the key this host trusts for it (§19.4)
	Version     string    // the sunstack it last reported; this host's own for this host
	Machine     string    // its machine's host name, when it reported one
	SSH         string    // this host's SSH view of it (core.SSHLine), set when the view is built
	SSHBack     string    // its SSH view of this host, from its snapshot
	KeyChanged  bool      // the roster shows another key; its mail is refused until org trust
}

// OrgView is the org from the local files only: drawing it never needs the
// network.
type OrgView struct {
	OrgName string
	HubName string
	Hosts   []*HostView // the hub first, then this host, then by name
	Outbox  int         // this host's mail not yet at the hub
}

// LoadView reads the org view. It returns nil when this host is in no org.
func LoadView(now time.Time) *OrgView {
	c, err := LoadConfig()
	if err != nil || c == nil {
		return nil
	}
	me := core.ThisHost().ID
	var r *Roster
	hubTime, localTime := now, now
	if c.IsHub() {
		r, _ = store{hubDir(c.OrgID)}.liveRoster()
	} else {
		var cr cachedRoster
		if readJSON(filepath.Join(remoteDir(), "roster.json"), &cr) == nil && cr.Roster != nil {
			r = cr.Roster
			if t, ok := parseStamp(cr.HubTime); ok {
				hubTime = t
			}
			if t, ok := parseStamp(cr.LocalTime); ok {
				localTime = t
			}
		}
	}
	v := &OrgView{OrgName: c.OrgName, Outbox: len(jsonFiles(outboxDir()))}
	pins := loadPins()
	if r == nil {
		v.Hosts = []*HostView{{ID: me, Name: core.ThisHost().Name, You: true, Hub: c.IsHub(), State: "live"}}
		return v
	}
	// Ages on the hub's clock, advanced on ours since we heard from it.
	since := now.Sub(localTime)
	if since < 0 {
		since = 0
	}
	for _, h := range r.Hosts {
		hv := &HostView{ID: h.ID, Name: h.Name, Hub: h.Hub, You: h.ID == me, CanSend: h.CanSend, Waiting: h.Waiting, State: "not synced yet", Version: h.Version, Machine: h.Machine}
		if !hv.You {
			hv.SSH = core.SSHLine(h.ID)
		}
		if hv.You {
			hv.Version, hv.Machine = Version, core.MachineName()
		}
		if h.Hub {
			v.HubName = h.Name
		}
		if p := pins[h.ID]; p != nil {
			hv.Fingerprint, hv.KeyChanged = p.Keys.Fingerprint(), p.Changed != nil
		} else if h.Keys.valid() {
			hv.Fingerprint = h.Keys.Fingerprint()
		}
		if t, ok := parseStamp(h.Contact); ok {
			hv.ContactAge = clampAge(hubTime.Sub(t) + since)
			hv.State = stateOf(hv.ContactAge)
		}
		if hv.You {
			hv.State, hv.ContactAge = "live", 0
		} else {
			loadSnap(c, h.ID, now, hv)
			hv.SSHBack = sshBack(hv, me)
		}
		v.Hosts = append(v.Hosts, hv)
	}
	sort.SliceStable(v.Hosts, func(i, j int) bool {
		a, b := v.Hosts[i], v.Hosts[j]
		if a.Hub != b.Hub {
			return a.Hub
		}
		if a.You != b.You {
			return a.You
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return v
}

func loadSnap(c *Config, id string, now time.Time, hv *HostView) {
	var sn Snap
	hubTime, localTime := now, now
	if c.IsHub() {
		if readJSON(store{hubDir(c.OrgID)}.snapFile(id), &sn) != nil {
			return
		}
	} else {
		var cs cachedSnap
		if readJSON(filepath.Join(remoteDir(), id+".json"), &cs) != nil {
			return
		}
		sn = cs.Snap
		if t, ok := parseStamp(cs.HubTime); ok {
			hubTime = t
		}
		if t, ok := parseStamp(cs.LocalTime); ok {
			localTime = t
		}
	}
	var o core.Org
	if json.Unmarshal(sn.Snapshot, &o) != nil {
		return
	}
	hv.Org, hv.HasSnap = &o, true
	if t, ok := parseStamp(sn.ReceivedAt); ok {
		hv.SnapAge = clampAge(hubTime.Sub(t) + now.Sub(localTime))
	}
}

// sshBack is a host's SSH view of this host, from its last snapshot: the
// other direction of the SSH line, observed only there.
func sshBack(hv *HostView, me string) string {
	if hv.Org == nil {
		return ""
	}
	for _, p := range hv.Org.SSH {
		if p.Host == me {
			line := "ssh from " + hv.Name + ": " + p.Text()
			if hv.State != "live" {
				line += " (last known)"
			}
			return line
		}
	}
	return ""
}

func clampAge(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d
}

func stateOf(age time.Duration) string {
	switch {
	case age < LiveFor:
		return "live"
	case age < OfflineFor:
		return "stale"
	}
	return "offline"
}

// Age prints a duration the way the views show it: 4s, 3m, 2h, 5d.
func Age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// Label is a host's heading: "server (live, 4s)".
func (h *HostView) Label() string {
	name := h.Title()
	switch {
	case h.You:
		return name + " (this host)"
	case h.State == "not synced yet":
		return name + " (not synced yet)"
	}
	return fmt.Sprintf("%s (%s, %s)", name, h.State, Age(h.ContactAge))
}

// Title is the host's org name, with its machine's host name when that
// differs.
func (h *HostView) Title() string {
	if h.Machine != "" && !strings.EqualFold(h.Machine, h.Name) {
		return h.Name + " · " + h.Machine
	}
	return h.Name
}

// Counts are a host's teams, sessions and items that need the user.
func (h *HostView) Counts() (teams, sessions, needs int) {
	if h.Org == nil {
		return 0, 0, 0
	}
	return OrgCounts(h.Org)
}

// OrgCounts counts a snapshot's teams, sessions and attention items.
func OrgCounts(o *core.Org) (teams, sessions, needs int) {
	for _, t := range o.Teams {
		for _, a := range t.Agents {
			sessions += len(a.Sessions)
		}
		sessions += len(t.Free)
	}
	for _, g := range o.Free {
		sessions += len(g.Sessions)
	}
	return len(o.Teams), sessions, len(o.Attention)
}

// Split separates another host's snapshot from this host's scan. A team
// whose files sync between hosts (git) has the same team ID on both: it is
// one team, returned in shared so its sessions can join this host's copy.
// own keeps the rest, and drops the warnings this host reports too. A team
// without a TEAM file is keyed by its root, so it is never shared. The
// snapshot itself is not changed.
func Split(remote, local *core.Org) (own *core.Org, shared []*core.OrgTeam) {
	o := *remote
	o.Teams, o.Attention = nil, nil
	here := map[string]bool{}
	seen := map[string]bool{}
	if local != nil {
		for _, t := range local.Teams {
			here[t.ID] = t.ID != ""
		}
		for _, a := range local.Attention {
			seen[a] = true
		}
	}
	for _, t := range remote.Teams {
		if here[t.ID] {
			shared = append(shared, t)
		} else {
			o.Teams = append(o.Teams, t)
		}
	}
	for _, a := range remote.Attention {
		if !seen[a] {
			o.Attention = append(o.Attention, a)
		}
	}
	return &o, shared
}

// Text renders the other hosts for `sunstack org`, in the same views as the
// local host. A team this host also has shows there only its sessions.
func (v *OrgView) Text(view string, local *core.Org) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nOther hosts (org %s, hub %s)\n", v.OrgName, v.HubName)
	n := 0
	for _, h := range v.Hosts {
		if h.You {
			continue
		}
		n++
		fmt.Fprintf(&b, "\n== %s ==\n", h.Label())
		if h.SSH != "" {
			fmt.Fprintln(&b, h.SSH)
		}
		if h.SSHBack != "" {
			fmt.Fprintln(&b, h.SSHBack)
		}
		if h.Org == nil {
			b.WriteString("  no snapshot yet\n")
			continue
		}
		if h.KeyChanged {
			fmt.Fprintf(&b, "  its key changed: mail from it is refused until you compare fingerprints (sunstack org keys) and run sunstack org trust %s\n", h.Name)
		}
		if h.State != "live" {
			fmt.Fprintf(&b, "  last known state, %s old\n", Age(h.SnapAge))
		}
		own, shared := Split(h.Org, local)
		o := *own
		o.Notes = nil
		var names []string
		for _, t := range shared {
			names = append(names, t.Name)
			if st := sessionsOnly(t); st != nil {
				o.Teams = append(o.Teams, st)
			}
		}
		o.Host.Name = h.Name // the org's name for it, as in the heading
		if len(names) > 0 && len(o.Teams) == 0 && len(o.Free) == 0 {
			// Nothing of its own: no "no teams" line for a host whose
			// teams are all here.
			if len(o.Attention) > 0 {
				b.WriteString(o.Text("attention"))
			}
			fmt.Fprintf(&b, "  no sessions; its teams are also on this host: %s\n", strings.Join(names, ", "))
			continue
		}
		b.WriteString(o.Text(view))
		if len(names) > 0 {
			fmt.Fprintf(&b, "  also on this host: %s (above, only their sessions on %s)\n", strings.Join(names, ", "), h.Name)
		}
	}
	if n == 0 {
		b.WriteString("  no other hosts yet\n")
	}
	if v.Outbox > 0 {
		fmt.Fprintf(&b, "\n%d message(s) to other hosts wait in the outbox (the hub was not reached)\n", v.Outbox)
	}
	return b.String()
}

// sessionsOnly is a shared team cut down to its agents with sessions and
// its free sessions; nil when it has none.
func sessionsOnly(t *core.OrgTeam) *core.OrgTeam {
	c := *t
	c.Agents, c.Objectives, c.Issues = nil, nil, nil
	for _, a := range t.Agents {
		if len(a.Sessions) > 0 {
			c.Agents = append(c.Agents, a)
		}
	}
	if len(c.Agents) == 0 && len(c.Free) == 0 {
		return nil
	}
	return &c
}

// HostID resolves an org host by its name or machine name, from the cached
// roster.
func HostID(name string) (id, orgName string, ok bool) {
	if r := cachedHosts(); r != nil {
		if h := r.byName(name); h != nil {
			return h.ID, h.Name, true
		}
	}
	return "", "", false
}

// HostName is an org host's name by its ID, or the ID when unknown.
func HostName(id string) string {
	if r := cachedHosts(); r != nil {
		for _, h := range r.Hosts {
			if h.ID == id {
				return h.Name
			}
		}
	}
	return id
}
