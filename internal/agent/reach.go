package agent

import (
	"encoding/binary"
	"io"
	"net"
	"time"
)

// DefaultProbeTimeout bounds a reachability probe, matching the login script's
// `timeout 2 ssh-add -l`.
const DefaultProbeTimeout = 2 * time.Second

// maxFrame caps the response length we will read, so a malformed or hostile peer
// cannot make us allocate unbounded memory. We only need the first payload byte.
const maxFrame = 256 << 10

// SocketProber probes a real ssh-agent by dialing its unix socket and issuing a
// minimal request-identities ping. A valid identities-answer — regardless of how
// many keys it lists — means the agent is healthy.
type SocketProber struct {
	// Timeout bounds dial + request + response; zero means DefaultProbeTimeout.
	Timeout time.Duration
}

// Reachable reports whether an ssh-agent answers on socket.
func (p SocketProber) Reachable(socket string) bool {
	if socket == "" {
		return false
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultProbeTimeout
	}
	conn, err := net.DialTimeout("unix", socket, timeout)
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return false
	}
	return identitiesAnswered(conn)
}

// identitiesAnswered sends SSH_AGENTC_REQUEST_IDENTITIES and reports whether the
// peer replies with an SSH_AGENT_IDENTITIES_ANSWER message.
func identitiesAnswered(conn io.ReadWriter) bool {
	req := [...]byte{0, 0, 0, 1, msgRequestIdentities}
	if _, err := conn.Write(req[:]); err != nil {
		return false
	}
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return false
	}
	length := binary.BigEndian.Uint32(header[:])
	if length < 1 || length > maxFrame {
		return false
	}
	var msgType [1]byte
	if _, err := io.ReadFull(conn, msgType[:]); err != nil {
		return false
	}
	return msgType[0] == msgIdentitiesAnswer
}
