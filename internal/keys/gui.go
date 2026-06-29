package keys

// GUIEnv is the subset of the environment that GUI detection reads.
type GUIEnv struct {
	WaylandDisplay string // $WAYLAND_DISPLAY
	Display        string // $DISPLAY
}

// GUIAvailable reports whether a graphical passphrase prompt can be shown: a
// usable graphical session AND an installed prompter. When it is false the loader
// falls back to letting ssh-add prompt on the terminal.
func GUIAvailable(env GUIEnv, r Runner, p Prompter) bool {
	return hasGraphicalSession(env, r) && p.Available()
}

// hasGraphicalSession reports whether a display server is reachable: a Wayland
// compositor advertised by WAYLAND_DISPLAY, or an X server that answers `xset q`.
// Checking xset (rather than DISPLAY alone) rejects a stale DISPLAY pointing at a
// dead server; a missing xset binary is treated as no X session.
func hasGraphicalSession(env GUIEnv, r Runner) bool {
	if env.WaylandDisplay != "" {
		return true
	}
	if env.Display == "" {
		return false
	}
	res, err := r.Run(Cmd{Name: "xset", Args: []string{"q"}})
	return err == nil && res.Code == 0
}
