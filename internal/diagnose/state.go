package diagnose

import "github.com/OrbintSoft/sshakku/internal/agent"

// State names the agent-lifecycle situation the report observes, following the
// five states the login path resolves (clean, ours-healthy, ours-zombie,
// foreign-healthy, disaster). The doctor only reports the state; the login path
// is what acts on it.
type State int

const (
	// StateUnknown is the zero value, before classification.
	StateUnknown State = iota
	// StateClean — nothing is running; a login starts fresh.
	StateClean
	// StateOursHealthy — our agent answers on the fixed socket.
	StateOursHealthy
	// StateOursZombie — only dead remnants of our agent remain.
	StateOursZombie
	// StateForeignHealthy — a single agent we did not start is answering.
	StateForeignHealthy
	// StateDisaster — several agents answer at once; the situation is mixed.
	StateDisaster
)

func (s State) String() string {
	switch s {
	case StateClean:
		return "A — clean (no agent running)"
	case StateOursHealthy:
		return "B — ours healthy"
	case StateOursZombie:
		return "C — ours zombie (dead remnant)"
	case StateForeignHealthy:
		return "D — foreign agent serving"
	case StateDisaster:
		return "E — disaster (multiple agents)"
	default:
		return "unknown"
	}
}

// classifyState maps the gathered agents to a single lifecycle state, in the same
// precedence order the login path resolves: several live agents are a disaster, a
// lone healthy agent is ours or foreign, and with nothing live the question is
// only whether dead remnants of ours linger.
func classifyState(r Report) State {
	var reachable, oursReach, otherReach, deadOurs int
	for _, a := range r.Agents {
		if a.Reachable {
			reachable++
			if a.Kind == agent.KindOurs {
				oursReach++
			} else {
				otherReach++
			}
			continue
		}
		if a.Socket != "" && (a.Kind == agent.KindOurs || a.Kind == agent.KindLegacy) {
			deadOurs++
		}
	}

	switch {
	case reachable > 1:
		return StateDisaster
	case oursReach == 1:
		return StateOursHealthy
	case otherReach == 1:
		return StateForeignHealthy
	case deadOurs > 0 || r.RecordedPID != 0:
		return StateOursZombie
	default:
		return StateClean
	}
}

// recommend returns the remediation for a state, phrased around what actually
// heals it today: opening a login shell, whose init reaps, starts, or adopts as
// the state requires.
func recommend(s State) string {
	switch s {
	case StateClean:
		return "no agent is running; a new login shell starts one and loads your keys"
	case StateOursHealthy:
		return "the agent is healthy; no action needed"
	case StateOursZombie:
		return "a stale agent of ours is dead; a new login shell reaps it and restarts on the fixed socket"
	case StateForeignHealthy:
		return "a foreign agent is serving you; a new login shell adopts it and reports the anomaly — investigate who started it if this is unexpected"
	case StateDisaster:
		return "several agents are running at once; a new login shell settles on one healthy agent and reaps the dead"
	default:
		return ""
	}
}
