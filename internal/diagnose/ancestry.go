package diagnose

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// maxAncestry bounds how far up the process tree we walk, so a pathological or
// looping /proc can never make the report run away.
const maxAncestry = 8

// ProcInfo identifies one process in an agent's ancestry: its pid and the short
// name the kernel records in /proc/<pid>/stat (truncated to 15 bytes).
type ProcInfo struct {
	PID  int
	Name string
}

// AncestrySource reports a process's parent pid and short name. ProcfsAncestry is
// the real implementation; tests supply a fake.
type AncestrySource interface {
	Parent(pid int) (ppid int, name string, ok bool)
}

// ProcfsAncestry reads the process tree from a Linux procfs. Root is injectable
// for tests; empty means "/proc".
type ProcfsAncestry struct {
	Root string
}

// Parent returns the parent pid and short name of pid from /proc/<pid>/stat.
func (a ProcfsAncestry) Parent(pid int) (int, string, bool) {
	root := a.Root
	if root == "" {
		root = "/proc"
	}
	b, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, "", false
	}
	name, ppid, ok := parseStat(b)
	if !ok {
		return 0, "", false
	}
	return ppid, name, true
}

// parseStat pulls the comm and ppid from a /proc/<pid>/stat line. comm is wrapped
// in parentheses and may itself contain spaces or ')', so we split on the final
// ')': everything before it is "pid (comm", and the space-separated fields after
// it begin with the state and then the ppid.
func parseStat(b []byte) (comm string, ppid int, ok bool) {
	s := string(b)
	open := strings.IndexByte(s, '(')
	end := strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return "", 0, false
	}
	comm = s[open+1 : end]
	fields := strings.Fields(s[end+1:])
	if len(fields) < 2 {
		return comm, 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return comm, 0, false
	}
	return comm, ppid, true
}

// ancestry walks the process tree from pid toward pid 1, returning the chain of
// processes (the pid itself first, then each parent up to init). It is bounded in
// depth and guards against a cycle, so a hostile or racing /proc cannot loop it.
func ancestry(pid int, src AncestrySource) []ProcInfo {
	if src == nil {
		return nil
	}
	var chain []ProcInfo
	seen := map[int]bool{}
	for cur := pid; cur >= 1 && !seen[cur] && len(chain) < maxAncestry; {
		seen[cur] = true
		ppid, name, ok := src.Parent(cur)
		if !ok {
			break
		}
		chain = append(chain, ProcInfo{PID: cur, Name: name})
		if ppid < 1 {
			break
		}
		cur = ppid
	}
	return chain
}

// startedBy attributes an agent to whoever launched it: the nearest ancestor
// (past the agent process itself) that matches a known session launcher, or the
// immediate parent's name when none is recognised. It returns false when the
// ancestry is too shallow to attribute anything.
func startedBy(chain []ProcInfo) (string, bool) {
	if len(chain) < 2 {
		return "", false
	}
	ancestors := chain[1:] // skip the agent process itself
	// A daemon double-forks and is reparented to init (pid 1); its real launcher
	// is then gone from the process tree, so ancestry cannot attribute it. Say so
	// rather than crediting init.
	if ancestors[0].PID == 1 {
		return "an unknown launcher (daemonized, reparented to init)", true
	}
	for _, p := range ancestors {
		if label, known := launcherLabel(p.Name); known {
			return label, true
		}
	}
	// Nothing recognised: fall back to naming the immediate parent.
	return ancestors[0].Name, true
}

// launcherLabel maps a known launcher's short (15-byte-truncated) comm to a
// friendly description. The truncated forms are what /proc actually reports.
func launcherLabel(comm string) (string, bool) {
	switch comm {
	case "systemd":
		return "systemd (user or system manager)", true
	case "gnome-keyring-d":
		return "gnome-keyring-daemon", true
	case "plasmashell", "ksmserver", "kwin_wayland", "kwin_x11", "startplasma-wa", "startplasma-x11":
		return "the KDE Plasma session", true
	case "gdm", "gdm-session-wor", "gdm-x-session", "gdm-wayland-ses":
		return "the GNOME display manager (GDM)", true
	case "sddm", "sddm-helper", "sddm-greeter":
		return "the SDDM display manager", true
	case "lightdm":
		return "the LightDM display manager", true
	case "sshd", "sshd-session":
		return "an SSH login session (sshd)", true
	case "login":
		return "a console login", true
	case "bash", "zsh", "fish", "sh", "dash":
		return "a login shell (" + comm + ")", true
	default:
		return "", false
	}
}

// chainString renders an ancestry chain as "name(pid) ← name(pid) ← …".
func chainString(chain []ProcInfo) string {
	parts := make([]string, len(chain))
	for i, p := range chain {
		parts[i] = p.Name + "(" + strconv.Itoa(p.PID) + ")"
	}
	return strings.Join(parts, " ← ")
}
