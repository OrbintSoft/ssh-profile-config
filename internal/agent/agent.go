// Package agent tends the user's ssh-agent: it probes whether an agent answers on
// a socket, starts one on the fixed socket, reaps dead agents and sockets, and
// adopts a healthy agent started by something else. It never reimplements the
// agent — it only manages the lifecycle of OpenSSH's ssh-agent.
package agent

// ssh-agent wire-protocol message types we use (OpenSSH PROTOCOL.agent).
const (
	msgRequestIdentities = 11 // SSH_AGENTC_REQUEST_IDENTITIES
	msgIdentitiesAnswer  = 12 // SSH_AGENT_IDENTITIES_ANSWER
)

// Prober reports whether a usable ssh-agent answers on a unix socket path.
// Reachable mirrors `ssh-add -l`: an agent with zero keys is still healthy.
type Prober interface {
	Reachable(socket string) bool
}
