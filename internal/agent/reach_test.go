package agent

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeAgent starts an in-process unix listener that handles each connection with
// reply, and returns its socket path.
func fakeAgent(t *testing.T, reply func(net.Conn)) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			reply(c)
			_ = c.Close()
		}
	}()
	return sock
}

// drainRequest reads one framed request so the prober's write completes.
func drainRequest(c net.Conn) {
	var hdr [4]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return
	}
	_, _ = io.CopyN(io.Discard, c, int64(binary.BigEndian.Uint32(hdr[:])))
}

// replyIdentities answers a request with an identities-answer listing nkeys keys
// (the keys themselves are omitted — the prober only inspects the message type).
func replyIdentities(nkeys uint32) func(net.Conn) {
	return func(c net.Conn) {
		drainRequest(c)
		payload := []byte{msgIdentitiesAnswer, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(payload[1:], nkeys)
		frame := make([]byte, 4+len(payload))
		binary.BigEndian.PutUint32(frame, uint32(len(payload)))
		copy(frame[4:], payload)
		_, _ = c.Write(frame)
	}
}

func TestSocketProberReachable(t *testing.T) {
	p := SocketProber{Timeout: time.Second}

	t.Run("healthy with keys", func(t *testing.T) {
		if !p.Reachable(fakeAgent(t, replyIdentities(2))) {
			t.Fatal("want reachable")
		}
	})
	t.Run("healthy but empty", func(t *testing.T) {
		if !p.Reachable(fakeAgent(t, replyIdentities(0))) {
			t.Fatal("want reachable for a live agent with no keys")
		}
	})
	t.Run("wrong reply type", func(t *testing.T) {
		sock := fakeAgent(t, func(c net.Conn) {
			drainRequest(c)
			_, _ = c.Write([]byte{0, 0, 0, 1, 99}) // not identities-answer
		})
		if p.Reachable(sock) {
			t.Fatal("want unreachable on an unexpected message type")
		}
	})
	t.Run("accept then close", func(t *testing.T) {
		sock := fakeAgent(t, func(net.Conn) {}) // reply nothing; conn is closed
		if p.Reachable(sock) {
			t.Fatal("want unreachable when the peer sends nothing")
		}
	})
	t.Run("empty path", func(t *testing.T) {
		if p.Reachable("") {
			t.Fatal("want unreachable for an empty path")
		}
	})
	t.Run("missing socket", func(t *testing.T) {
		if p.Reachable(filepath.Join(t.TempDir(), "nope.sock")) {
			t.Fatal("want unreachable for a missing socket")
		}
	})
	t.Run("not a socket", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "regular")
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if p.Reachable(f) {
			t.Fatal("want unreachable for a regular file")
		}
	})
}

// SocketProber must satisfy Prober.
var _ Prober = SocketProber{}
