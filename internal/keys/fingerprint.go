package keys

import "strings"

// FileFingerprint returns the SHA256 fingerprint of the key at path, read with
// `ssh-keygen -lf <path>`. A key ssh-keygen cannot read (wrong format, no such
// file) yields an empty fingerprint and no error — the loader then treats it as
// not-yet-loaded rather than aborting the whole run.
func FileFingerprint(r Runner, path string) (string, error) {
	res, err := r.Run(Cmd{Name: "ssh-keygen", Args: []string{"-lf", path}})
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", nil
	}
	return fingerprintField(string(res.Stdout)), nil
}

// AgentFingerprints returns the set of fingerprints currently loaded in the
// agent, read with `ssh-add -l`. An empty agent (exit 1) or no agent at all
// (exit 2) yields an empty set, not an error — mirroring the bash snapshot, where
// a missing or empty agent simply means nothing is loaded yet.
func AgentFingerprints(r Runner) (map[string]bool, error) {
	res, err := r.Run(Cmd{Name: "ssh-add", Args: []string{"-l"}})
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool)
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		if fp := fingerprintField(line); fp != "" {
			set[fp] = true
		}
	}
	return set, nil
}

// fingerprintField extracts the hash field of a single `ssh-keygen -lf` /
// `ssh-add -l` line, whose format is "<bits> <HASH> <comment> (<type>)". It
// returns "" when the line carries no hash (e.g. "The agent has no identities."),
// so status lines are never mistaken for fingerprints.
func fingerprintField(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}
	hash := fields[1]
	if strings.HasPrefix(hash, "SHA256:") || strings.HasPrefix(hash, "MD5:") {
		return hash
	}
	return ""
}
