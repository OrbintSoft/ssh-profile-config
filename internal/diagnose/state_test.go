package diagnose

import (
	"testing"

	"github.com/OrbintSoft/sshakku/internal/agent"
)

func TestClassifyState(t *testing.T) {
	ours := func(reachable bool) AgentView {
		return AgentView{Kind: agent.KindOurs, Socket: fixed, Reachable: reachable}
	}
	foreign := func(reachable bool) AgentView {
		return AgentView{Kind: agent.KindForeign, Socket: "/tmp/f.sock", Reachable: reachable}
	}
	legacyAgent := AgentView{Kind: agent.KindLegacy, Socket: legacy + "/ssh-agent.sock", Reachable: true}

	cases := []struct {
		name string
		r    Report
		want State
	}{
		{"clean, nothing", Report{}, StateClean},
		{"clean, only a dead foreign", Report{Agents: []AgentView{foreign(false)}}, StateClean},
		{"ours healthy", Report{Agents: []AgentView{ours(true)}}, StateOursHealthy},
		{"foreign healthy", Report{Agents: []AgentView{foreign(true)}}, StateForeignHealthy},
		{"legacy healthy counts as foreign", Report{Agents: []AgentView{legacyAgent}}, StateForeignHealthy},
		{"ours zombie, dead socket", Report{Agents: []AgentView{ours(false)}}, StateOursZombie},
		{"ours zombie, recorded pid only", Report{RecordedPID: 123}, StateOursZombie},
		{"disaster, two live", Report{Agents: []AgentView{ours(true), foreign(true)}}, StateDisaster},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyState(c.r); got != c.want {
				t.Errorf("classifyState = %v, want %v", got, c.want)
			}
		})
	}
}

func TestRecommend(t *testing.T) {
	for _, s := range []State{StateClean, StateOursHealthy, StateOursZombie, StateForeignHealthy, StateDisaster} {
		if recommend(s) == "" {
			t.Errorf("recommend(%v) is empty", s)
		}
	}
	if recommend(StateUnknown) != "" {
		t.Error("recommend(StateUnknown) should be empty")
	}
}

func TestStateString(t *testing.T) {
	cases := map[State]string{
		StateClean:          "A —",
		StateOursHealthy:    "B —",
		StateOursZombie:     "C —",
		StateForeignHealthy: "D —",
		StateDisaster:       "E —",
		StateUnknown:        "unknown",
	}
	for s, prefix := range cases {
		if got := s.String(); got[:len(prefix)] != prefix {
			t.Errorf("State(%d).String() = %q, want it to start with %q", s, got, prefix)
		}
	}
}
