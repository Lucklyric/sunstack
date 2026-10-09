package core

import (
	"sort"
	"strings"
	"time"
)

// The plan behind the Board tab (§21.1): objectives, the key results that
// serve them, and each key result's direct needs. Only key results count;
// asks and directives are never progress.

// KRStatuses are the statuses of a key result, in the order that picks the
// one shown when several apply.
var KRStatuses = []string{"done", "blocked", "overdue", "stale", "active", "planned", "open"}

// KRView is one key result with its status.
type KRView struct {
	Item
	Status     string   // the first of KRStatuses that applies
	Conditions []string // every status that applies, in that order
	Needs      []NeedView
}

// NeedView is one direct need of a key result.
type NeedView struct {
	Raw    string // as written on the board
	Ref    string // owner#key of the target; empty for free text and missing targets
	Status string // the target's status, an ask's "answered" or "waiting on the user", or "waiting"
	Done   bool
}

// ObjectiveView is one objective with its key results. Key is empty for the
// key results that serve no known objective.
type ObjectiveView struct {
	Key, Text, Due string
	KRs            []*KRView // the user's first, then by owner and number
	Done           int
	Status         string // "no key results", "all done", or the first status among the open ones
}

// Plan builds the Board tab's tree from the boards.
func (b *Boards) Plan(today time.Time) []*ObjectiveView {
	byRef := map[string]Item{}
	var krs []*KRView
	add := func(it Item) {
		byRef[it.Ref()] = it
		if strings.HasPrefix(it.Key, "KR") {
			krs = append(krs, &KRView{Item: it})
		}
	}
	var objs []*ObjectiveView
	known := map[string]*ObjectiveView{}
	for _, it := range b.Team {
		switch it.Section {
		case "Objectives":
			if strings.HasPrefix(it.Key, "O") && known[it.Key] == nil {
				o := &ObjectiveView{Key: it.Key, Text: it.Text, Due: it.Due}
				known[it.Key] = o
				objs = append(objs, o)
			}
		case "User":
			add(it)
		}
	}
	for _, id := range sortedIDs(b.Agents) {
		for _, it := range b.Agents[id] {
			add(it)
		}
	}

	// Pass one: every status but blocked. Pass two: blocked, from the direct
	// needs' done flags only, so a cycle of needs cannot loop.
	date := today.Format("2006-01-02")
	for _, k := range krs {
		k.Conditions = baseConditions(k.Item, today, date)
	}
	view := map[string]*KRView{}
	for _, k := range krs {
		view[k.Ref()] = k
	}
	for _, k := range krs {
		for _, n := range k.Item.Needs {
			k.Needs = append(k.Needs, resolveNeed(k.Item, n, byRef))
		}
		if k.Done {
			continue
		}
		for _, nv := range k.Needs {
			if !nv.Done {
				k.Conditions = append([]string{"blocked"}, k.Conditions...)
				break
			}
		}
	}
	for _, k := range krs {
		k.Status = k.Conditions[0]
		for i, nv := range k.Needs {
			if t := view[nv.Ref]; t != nil {
				k.Needs[i].Status = t.Conditions[0]
			}
		}
	}

	none := &ObjectiveView{Text: "no objective"}
	for _, k := range krs {
		o := known[k.Obj]
		if o == nil {
			o = none
		}
		o.KRs = append(o.KRs, k)
	}
	if len(none.KRs) > 0 {
		objs = append(objs, none)
	}
	rank := map[string]int{}
	for i, s := range KRStatuses {
		rank[s] = i
	}
	for _, o := range objs {
		sort.SliceStable(o.KRs, func(i, j int) bool {
			a, b := o.KRs[i], o.KRs[j]
			if (a.Owner == "user") != (b.Owner == "user") {
				return a.Owner == "user"
			}
			if a.Owner != b.Owner {
				return a.Owner < b.Owner
			}
			return keyNum(a.Key) < keyNum(b.Key)
		})
		first := ""
		for _, k := range o.KRs {
			if k.Done {
				o.Done++
			} else if first == "" || rank[k.Status] < rank[first] {
				first = k.Status
			}
		}
		switch {
		case len(o.KRs) == 0:
			o.Status = "no key results"
		case first == "":
			o.Status = "all done"
		default:
			o.Status = first
		}
	}
	return objs
}

// baseConditions are the statuses of a key result apart from blocked.
func baseConditions(it Item, today time.Time, date string) []string {
	if it.Done {
		return []string{"done"}
	}
	var out []string
	if _, err := time.Parse("2006-01-02", it.Due); err == nil && it.Due < date {
		out = append(out, "overdue")
	}
	switch it.Section {
	case "Now":
		if d, err := time.Parse("2006-01-02", it.Date); err == nil && today.Sub(d) > staleDays*24*time.Hour {
			out = append(out, "stale")
		}
		out = append(out, "active")
	case "Next":
		out = append(out, "planned")
	default:
		out = append(out, "open")
	}
	return out
}

// resolveNeed finds what a need names, the way board's findings do: a bare
// KR<n> is the owner's own, user#Q<n> is the owner's ask, and anything else
// that names no entry is a waiting reason.
func resolveNeed(it Item, n string, byRef map[string]Item) NeedView {
	nv := NeedView{Raw: n, Status: "waiting"}
	if q, isAsk := strings.CutPrefix(n, "user#"); isAsk && strings.HasPrefix(q, "Q") {
		if ask, ok := byRef[it.Owner+"#"+q]; ok {
			nv.Ref, nv.Done = ask.Ref(), ask.Done
			nv.Status = "waiting on the user"
			if ask.Done {
				nv.Status = "answered"
			}
		}
		return nv
	}
	if keyRe.MatchString(n) {
		n = it.Owner + "#" + n
	}
	if dep, ok := byRef[n]; ok {
		nv.Ref, nv.Done = dep.Ref(), dep.Done
		if dep.Done {
			nv.Status = "done"
		}
	}
	return nv
}

func sortedIDs(m map[string][]Item) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
