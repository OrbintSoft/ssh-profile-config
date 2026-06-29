package agent

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcKind labels an ssh-agent process by who owns its lifecycle.
type ProcKind int

const (
	// KindForeign is an agent we did not start (or one started without -a).
	KindForeign ProcKind = iota
	// KindOurs is an agent listening on our fixed socket.
	KindOurs
	// KindLegacy is a pre-sshakku `ssh-agent -a ~/.ssh/agent/...`.
	KindLegacy
)

func (k ProcKind) String() string {
	switch k {
	case KindOurs:
		return "ours"
	case KindLegacy:
		return "legacy"
	default:
		return "foreign"
	}
}

// AgentProc is a running ssh-agent process discovered under procfs.
type AgentProc struct {
	PID    int      // process id
	UID    int      // owning real uid, or -1 if unknown (gates same-user reaping)
	Socket string   // the `-a <path>` bind address, or "" if started without one
	Args   []string // full argv, kept for diagnostics and anomaly reporting
}

// Inspector enumerates ssh-agent processes from a Linux procfs tree. ProcRoot is
// injectable so tests can point at a fake tree; empty means "/proc".
type Inspector struct {
	ProcRoot string
}

// Agents returns the ssh-agent processes currently visible under procfs. A
// process that disappears mid-scan is skipped, not reported as an error.
func (in Inspector) Agents() ([]AgentProc, error) {
	root := in.ProcRoot
	if root == "" {
		root = "/proc"
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read procfs %s: %w", root, err)
	}
	var procs []AgentProc
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a pid directory (e.g. "self", "net")
		}
		dir := filepath.Join(root, e.Name())
		argv, err := readCmdline(filepath.Join(dir, "cmdline"))
		if err != nil || len(argv) == 0 {
			continue // process gone, or a kernel thread with empty cmdline
		}
		if filepath.Base(argv[0]) != "ssh-agent" {
			continue
		}
		procs = append(procs, AgentProc{
			PID:    pid,
			UID:    readStatusUID(filepath.Join(dir, "status")),
			Socket: socketArg(argv),
			Args:   argv,
		})
	}
	return procs, nil
}

// Classify labels an ssh-agent process from its bind socket alone: ours when it
// listens on fixedSock, legacy when it listens under legacyDir, foreign
// otherwise. A process started without -a has no socket and is foreign.
func Classify(p AgentProc, fixedSock, legacyDir string) ProcKind {
	switch {
	case p.Socket != "" && p.Socket == fixedSock:
		return KindOurs
	case isUnder(p.Socket, legacyDir):
		return KindLegacy
	default:
		return KindForeign
	}
}

// readCmdline reads a NUL-separated /proc/<pid>/cmdline into an argv slice.
func readCmdline(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	b = bytes.TrimRight(b, "\x00")
	if len(b) == 0 {
		return nil, nil
	}
	return strings.Split(string(b), "\x00"), nil
}

// readStatusUID returns the real uid from /proc/<pid>/status, or -1 when it
// cannot be determined. -1 never matches our uid, so it is safe for reaping.
func readStatusUID(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return -1
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return -1
		}
		uid, err := strconv.Atoi(fields[1])
		if err != nil {
			return -1
		}
		return uid
	}
	return -1
}

// socketArg extracts the `-a <path>` bind address from an ssh-agent argv,
// accepting both the separated "-a path" and the joined "-apath" forms.
func socketArg(argv []string) string {
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "-a":
			if i+1 < len(argv) {
				return argv[i+1]
			}
		case strings.HasPrefix(a, "-a") && len(a) > 2:
			return a[2:]
		}
	}
	return ""
}

// isUnder reports whether path lies inside dir.
func isUnder(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	return strings.HasPrefix(filepath.Clean(path), filepath.Clean(dir)+string(filepath.Separator))
}
