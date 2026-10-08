package hub

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// SpawnArgs is a spawn on another host (§20.4): an agent of a team, or a
// free session in a team or beside a session.
type SpawnArgs struct {
	Team   string `json:"team,omitempty"`
	Agent  string `json:"agent,omitempty"`
	Free   bool   `json:"free,omitempty"`
	Beside string `json:"beside,omitempty"` // a session ID
	Dir    string `json:"dir,omitempty"`    // a subfolder of the team
	Tool   string `json:"tool,omitempty"`
	Task   string `json:"task,omitempty"`
	Name   string `json:"name,omitempty"`
	Note   string `json:"note,omitempty"`
	Brief  string `json:"brief,omitempty"`
}

// SpawnResult is what a remote spawn reports: never the claim token.
type SpawnResult struct {
	Label   string `json:"label"`
	Place   string `json:"place"`
	Pane    string `json:"pane"`
	Running bool   `json:"running"`
}

var teamRe = regexp.MustCompile(`^[A-Za-z0-9._ -]{1,80}$`)

func spawnHandler(from, id string, raw json.RawMessage) (any, error) {
	var a SpawnArgs
	if json.Unmarshal(raw, &a) != nil {
		return nil, errors.New("bad spawn request")
	}
	if a.Free {
		o := core.FreeOptions{Tool: a.Tool, Name: a.Name, Note: a.Note, Dir: a.Dir, Remote: true, Request: id}
		switch {
		case a.Beside != "":
			if !core.IsSessionID(a.Beside) || a.Dir != "" {
				return nil, errors.New("beside a session is named by its session ID, with no --dir")
			}
			o.Beside = a.Beside
		case teamRe.MatchString(a.Team):
			p, err := core.ResolveTeam(a.Team)
			if err != nil {
				return nil, err
			}
			o.Root = p.Root
		default:
			return nil, errors.New("name a team, or a session to start beside")
		}
		r, err := core.SpawnFree(o)
		if err != nil {
			return nil, err
		}
		return SpawnResult{Label: r.Label, Place: r.Home, Pane: r.Pane, Running: r.Running}, nil
	}
	if !teamRe.MatchString(a.Team) || a.Agent == "" {
		return nil, errors.New("name a team and an agent")
	}
	if a.Brief != "" {
		if err := core.CheckBrief(a.Brief); err != nil {
			return nil, err
		}
	}
	p, err := core.ResolveTeam(a.Team)
	if err != nil {
		return nil, err
	}
	r, err := p.Spawn(core.SpawnOptions{Arg: a.Agent, Tool: a.Tool, Task: a.Task, Note: a.Note, Brief: a.Brief, Place: "team", FromHost: from, Request: id})
	if err != nil {
		return nil, err
	}
	return SpawnResult{Label: r.Name, Place: r.Home, Pane: r.Pane, Running: r.Running}, nil
}

// Spawn asks another host to start a session.
func Spawn(hostName string, a SpawnArgs) (*SpawnResult, error) {
	id, err := Request(hostName, "spawn", a, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	_ = FlushOnce() // the connector moves it on when one runs
	c, err := Await(id, 60*time.Second)
	if err != nil {
		return nil, err
	}
	var r SpawnResult
	if json.Unmarshal(c.Result, &r) != nil {
		return nil, failErr("bad_reply", "the spawn reply cannot be read")
	}
	return &r, nil
}
