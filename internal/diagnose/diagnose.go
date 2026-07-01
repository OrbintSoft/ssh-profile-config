// Package diagnose builds a read-only picture of the ssh-agent situation for the
// `sshakku doctor` command: which ssh-agent processes are running, which one (if
// any) is ours, whether each answers, and whether the shell's SSH_AUTH_SOCK is
// wired to a healthy agent. It only reads state — it never starts, signals, or
// reaps anything.
package diagnose

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/OrbintSoft/sshakku/internal/agent"
)

// logTailLines is how many trailing session-log lines the report shows.
const logTailLines = 10

// AgentSource enumerates the ssh-agent processes currently visible.
// agent.Inspector satisfies it; tests supply a fake.
type AgentSource interface {
	Agents() ([]agent.AgentProc, error)
}

// Inputs are the facts Gather reasons over, injected so it stays pure and
// testable — nothing here is read from the ambient process.
type Inputs struct {
	FixedSock string // the socket our agent binds (from the resolved layout)
	LegacyDir string // ~/.ssh/agent, for spotting a pre-sshakku agent
	StatePath string // agent.state, holding the pid of the agent we started
	EnvSock   string // SSH_AUTH_SOCK as this shell sees it
	LogFile   string // session log to tail
	OurUID    int    // the invoking user's uid, to tell same-user agents apart
}

// AgentView is one ssh-agent process as the report presents it.
type AgentView struct {
	PID       int
	UID       int // owning uid, or -1 when it could not be read
	Kind      agent.ProcKind
	Socket    string
	Reachable bool
	Ancestry  []ProcInfo // the process chain that launched it, agent first
}

// Report is the read-only picture the doctor presents.
type Report struct {
	FixedSock    string
	EnvSock      string
	EnvReachable bool
	OurUID       int
	RecordedPID  int // pid from agent.state, 0 when absent or unreadable
	Agents       []AgentView
	State        State
	Findings     []string
	LogTail      []string
	InspectErr   error // enumeration failed; the report is partial
}

// Gather inspects the agent situation described by in and returns the report,
// reading everything through src, prober, and anc so it never touches the real
// /proc or sockets in a test. A nil anc skips ancestry attribution. It mutates
// nothing.
func Gather(in Inputs, src AgentSource, prober agent.Prober, anc AncestrySource) Report {
	r := Report{
		FixedSock: in.FixedSock,
		EnvSock:   in.EnvSock,
		OurUID:    in.OurUID,
	}
	if in.EnvSock != "" {
		r.EnvReachable = prober.Reachable(in.EnvSock)
	}
	if st, err := agent.ReadState(in.StatePath); err == nil {
		r.RecordedPID = st.PID
	}

	procs, err := src.Agents()
	if err != nil {
		r.InspectErr = err
	}
	for _, p := range procs {
		r.Agents = append(r.Agents, AgentView{
			PID:       p.PID,
			UID:       p.UID,
			Kind:      agent.Classify(p, in.FixedSock, in.LegacyDir),
			Socket:    p.Socket,
			Reachable: p.Socket != "" && prober.Reachable(p.Socket),
			Ancestry:  ancestry(p.PID, anc),
		})
	}

	r.State = classifyState(r)
	r.LogTail = tailLines(in.LogFile, logTailLines)
	r.Findings = findings(in, r)
	return r
}

// findings turns the gathered facts into plain-language observations. It only
// describes what it sees; remediation guidance arrives with the fix path.
func findings(in Inputs, r Report) []string {
	var reachable, dead int
	for _, a := range r.Agents {
		switch {
		case a.Reachable:
			reachable++
		case a.Socket != "":
			dead++
		}
	}

	var f []string
	switch {
	case in.EnvSock == "":
		f = append(f, "SSH_AUTH_SOCK is unset — this shell cannot reach any agent")
	case !r.EnvReachable:
		f = append(f, fmt.Sprintf("SSH_AUTH_SOCK points at %s, which is not answering", in.EnvSock))
	case in.EnvSock != in.FixedSock:
		f = append(f, fmt.Sprintf("SSH_AUTH_SOCK is %s, not our fixed socket %s", in.EnvSock, in.FixedSock))
	}

	switch {
	case reachable == 0:
		f = append(f, "no ssh-agent is answering; a new login shell will start one")
	case reachable > 1:
		f = append(f, fmt.Sprintf("%d agents are answering; normally only one should serve you", reachable))
	}
	if dead > 0 {
		f = append(f, fmt.Sprintf("%d dead ssh-agent process(es) with a stale socket are lingering", dead))
	}
	for _, a := range r.Agents {
		if a.Kind != agent.KindForeign || !a.Reachable {
			continue
		}
		who := "an unknown launcher"
		if label, ok := startedBy(a.Ancestry); ok {
			who = label
		}
		f = append(f, fmt.Sprintf("a foreign ssh-agent (pid %d) started by %s is answering", a.PID, who))
	}
	if r.InspectErr != nil {
		f = append(f, fmt.Sprintf("could not enumerate processes: %v (report is partial)", r.InspectErr))
	}

	if len(f) == 0 {
		f = append(f, "no problems detected")
	}
	return f
}

// Format writes a human-readable rendering of r to w. It builds the whole report
// first and writes it once, so a short write cannot leave a half-printed report.
func Format(w io.Writer, r Report) {
	var b strings.Builder
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(&b, format, a...) }

	p("sshakku doctor — ssh-agent diagnostics\n\n")
	p("state: %s\n\n", r.State)
	p("fixed socket:  %s\n", orNone(r.FixedSock))
	p("SSH_AUTH_SOCK: %s%s\n", orUnset(r.EnvSock), envReachSuffix(r.EnvSock, r.EnvReachable))
	if r.RecordedPID != 0 {
		p("recorded pid:  %d (agent.state)\n", r.RecordedPID)
	}

	p("\nssh-agent processes (%d):\n", len(r.Agents))
	if len(r.Agents) == 0 {
		p("  (none)\n")
	}
	for _, a := range r.Agents {
		state := "dead"
		if a.Reachable {
			state = "reachable"
		}
		p("  pid %-7d %-7s %-9s %-6s %s\n",
			a.PID, a.Kind, state, uidNote(a.UID, r.OurUID), orNone(a.Socket))
		if label, ok := startedBy(a.Ancestry); ok {
			p("    started by %s\n", label)
			p("    %s\n", chainString(a.Ancestry))
		}
	}

	p("\nfindings:\n")
	for _, s := range r.Findings {
		p("  - %s\n", s)
	}

	if rec := recommend(r.State); rec != "" {
		p("\nrecommendation:\n  %s\n", rec)
	}

	if len(r.LogTail) > 0 {
		p("\nrecent log:\n")
		for _, line := range r.LogTail {
			p("  %s\n", line)
		}
	}

	_, _ = io.WriteString(w, b.String())
}

// uidNote labels an agent's owning uid, marking the invoking user's own agents.
func uidNote(uid, ourUID int) string {
	if uid < 0 {
		return "uid ?"
	}
	if uid == ourUID {
		return "you"
	}
	return "uid " + strconv.Itoa(uid)
}

// envReachSuffix annotates the SSH_AUTH_SOCK line with its reachability, and
// nothing when the variable is unset.
func envReachSuffix(sock string, reachable bool) string {
	if sock == "" {
		return ""
	}
	if reachable {
		return "  (reachable)"
	}
	return "  (not answering)"
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func orUnset(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}

// tailLines returns the last n non-empty-trailing lines of the file at path, or
// nil when the file is missing or empty. A read error is not surfaced: the log is
// a convenience, not a required input.
func tailLines(path string, n int) []string {
	if path == "" || n <= 0 {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	trimmed := strings.TrimRight(string(b), "\n")
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
