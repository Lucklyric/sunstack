package tui

import (
	"strings"
	"testing"

	"github.com/Lucklyric/sunstack/internal/core"
)

// Review r33-r1: a question shows as a question, and its reply as an answer.
func TestTaskDetailsQuestion(t *testing.T) {
	d := &core.TaskDetail{TaskInfo: &core.TaskInfo{ID: "q1", Type: "question", From: "builder.alice", To: "builder.bob", Goal: "CSV?"}, Body: "CSV?"}
	d.Replies = []core.ReplyDetail{{TaskReply: core.TaskReply{ID: "r1"}, From: "builder.bob", Body: "CSV."}}
	m := &model{}
	s := strings.Join(m.taskDetails(taskRow{t: d}, 80), "\n")
	for _, want := range []string{"Question", "type", "Answer r1"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "verified:") {
		t.Errorf("a question's answer uses task-proof wording:\n%s", s)
	}
}
