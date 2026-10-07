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
		hv := &HostView{ID: h.ID, Name: h.Name, Hub: h.Hub, You: h.ID == me, CanSend: h.CanSend, Waiting: h.Waiting, State: "not synced yet"}
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
	switch {
	case h.You:
		return h.Name + " (this host)"
	case h.State == "not synced yet":
		return h.Name + " (not synced yet)"
	}
	return fmt.Sprintf("%s (%s, %s)", h.Name, h.State, Age(h.ContactAge))
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

// Text renders the other hosts for `sunstack org`, in the same views as the
// local host.
func (v *OrgView) Text(view string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nOther hosts (org %s, hub %s)\n", v.OrgName, v.HubName)
	n := 0
	for _, h := range v.Hosts {
		if h.You {
			continue
		}
		n++
		fmt.Fprintf(&b, "\n== %s ==\n", h.Label())
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
		o := *h.Org
		o.Notes = nil
		b.WriteString(o.Text(view))
	}
	if n == 0 {
		b.WriteString("  no other hosts yet\n")
	}
	if v.Outbox > 0 {
		fmt.Fprintf(&b, "\n%d message(s) to other hosts wait in the outbox (the hub was not reached)\n", v.Outbox)
	}
	return b.String()
}
