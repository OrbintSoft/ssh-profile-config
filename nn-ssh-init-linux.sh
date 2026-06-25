#!/bin/bash

# SSH agent + key bootstrap, sourced from /etc/profile.d
#
# Incident 2026-06: OpenSSH 10.x moved the default ssh-agent socket from
# /tmp/ssh-XXX/agent.PID to ~/.ssh/agent/s.<base>.agent.<random>, i.e. the path
# now carries a RANDOM suffix that changes every time an agent is started.
# Combined with a check that killed a perfectly healthy (but still keyless)
# agent, this left the graphical session pointing at a dead socket.
#
# To stay robust across OpenSSH/Plasma updates this script:
#   * pins the agent to a FIXED socket path (ssh-agent -a), so the value we
#     export into the session never goes stale even if the agent is restarted;
#   * NEVER kills a healthy agent. `ssh-add -l` returns 1 for a reachable agent
#     that simply has no keys yet -- that is NOT a dead agent;
#   * decides "is this key already loaded?" by fingerprint, so no growing
#     state file is needed.

key_dir="$HOME/.ssh"
log_file="$key_dir/sessions.log"
key_prefix="SSH-Key"
ssh_askpass_script="/usr/local/bin/ssh-ask-pass.sh"
max_log_lines=100
max_attempts=3

agent_dir="$key_dir/agent"
agent_sock="$agent_dir/ssh-agent.sock"
agent_lock="$agent_dir/.start.lock"

log_message() {
	local level="$1"
	local message="$2"
	local print_message="${3:-false}"
	local line_count
	local file_cut
	local datetime
	local log_message
	datetime=$(date '+%Y-%m-%d %H:%M:%S')
	log_message="$datetime | [$level] $message"
	echo "$log_message" >>"$log_file"
	if [ "$print_message" = true ]; then
		echo "$log_message"
	fi
	line_count=$(wc -l <"$log_file")
	if [ "$line_count" -gt "$max_log_lines" ]; then
		file_cut=$(tail -n "$max_log_lines" "$log_file")
		echo "$file_cut" >"$log_file"
	fi
}

# Return 0 if a usable agent is reachable on the fixed socket.
# ssh-add -l exit codes: 0 = has keys, 1 = reachable but empty (both healthy),
# 2 = cannot connect, 124 = timed out (both = not usable -> needs a fresh agent).
ssh_agent_reachable() {
	[ -S "$agent_sock" ] || return 1
	SSH_AUTH_SOCK="$agent_sock" timeout 2 ssh-add -l >/dev/null 2>&1
	local rc=$?
	[ "$rc" -eq 0 ] || [ "$rc" -eq 1 ]
}

mkdir -p "$agent_dir"
chmod 700 "$agent_dir"

# Always pin this shell -- and, at login, the whole session -- to the fixed path.
export SSH_AUTH_SOCK="$agent_sock"
unset SSH_AGENT_PID

if ! ssh_agent_reachable; then
	# Serialize against other shells opening at the same moment (login burst).
	if command -v flock >/dev/null 2>&1; then
		exec {agent_lock_fd}>"$agent_lock"
		flock -w 5 "$agent_lock_fd" 2>/dev/null
	fi
	# Re-check inside the lock: another shell may have just started it.
	if ! ssh_agent_reachable; then
		rm -f "$agent_sock"
		if eval "$(ssh-agent -a "$agent_sock" -s)" >/dev/null 2>&1; then
			log_message "INFO" "Started ssh-agent on fixed socket $agent_sock (PID $SSH_AGENT_PID)"
		else
			log_message "ERROR" "Failed to start ssh-agent on $agent_sock" "true"
		fi
	fi
	if [ -n "${agent_lock_fd:-}" ]; then
		exec {agent_lock_fd}>&-
		unset agent_lock_fd
	fi
	# `ssh-agent -s` re-exported SSH_AUTH_SOCK to the same path; keep it pinned.
	export SSH_AUTH_SOCK="$agent_sock"
fi

gui_available=false
if [ -n "$DISPLAY" ] && command -v xset &>/dev/null && xset q &>/dev/null; then
	gui_available=true
fi

# Load keys only in interactive shells.
if [[ $- == *i* ]]; then
	# Snapshot of fingerprints already in the agent, to skip keys already loaded.
	loaded_fingerprints=$(ssh-add -l 2>/dev/null)

	find "$key_dir" -maxdepth 1 -type f -name 'id_*' ! -name '*.pub' | while read -r keyfile; do
		keyname=$(basename "$keyfile")
		key_fingerprint=$(ssh-keygen -lf "$keyfile" 2>/dev/null | awk '{print $2}')

		# Already in the agent? Nothing to do.
		if [ -n "$key_fingerprint" ] && printf '%s\n' "$loaded_fingerprints" | grep -qF "$key_fingerprint"; then
			log_message "INFO" "✅ $keyname already added to agent"
			continue
		fi

		attempts=0
		# Try to add the key with a maximum number of attempts.
		while [ $attempts -lt $max_attempts ]; do
			ssh_add_result=1
			if [ "$gui_available" = true ]; then
				wallet_key="$key_prefix-$keyname"
				# Look up the passphrase in the secret store.
				passphrase=$(secret-tool lookup service "$wallet_key" username "$USER")
				passphrase_stored=false
				if [[ -n "$passphrase" && ! "$passphrase" =~ ^[[:space:]]*$ ]]; then
					log_message "INFO" "🔐 Using stored passphrase for $keyname"
					passphrase_stored=true
				else
					# If the passphrase is not found, prompt the user.
					log_message "INFO" "❓ No stored entry for $keyname, prompting..." "true"
					if ! passphrase=$(kdialog --password "Enter passphrase for $keyname"); then
						log_message "ERROR" "❌ kdialog failed to get passphrase for $keyname" "true"
						break
					fi
				fi
				# Hand the passphrase to ssh-add via a short-lived keyctl entry.
				ssh_pass_uuid="$(uuidgen | tr -d '-')"
				if ! ssh_tmp_keyctl=$(keyctl add user "$ssh_pass_uuid" "$passphrase" @u); then
					log_message "ERROR" "❌ Failed to add keyctl for $keyname" "true"
					break
				fi
				if ! keyctl timeout "$ssh_tmp_keyctl" 60 >/dev/null; then
					log_message "ERROR" "❌ Failed to set keyctl timeout for $keyname"
					break
				fi
				# Add the key; ssh-ask-pass.sh reads the passphrase from keyctl.
				SSH_TEMP_KEYCTL="$ssh_tmp_keyctl" SSH_PASS_UUID="$ssh_pass_uuid" SSH_ASKPASS="$ssh_askpass_script" \
					setsid timeout 60 ssh-add "$keyfile" </dev/null
				ssh_add_result=$?
				# Store the passphrase only after a successful, first-time add.
				if [ "$passphrase_stored" = false ] && [ "$ssh_add_result" -eq 0 ]; then
					if ! echo "$passphrase" | secret-tool store --label="SSH Passphrase for $keyname" service "$wallet_key" username "$USER"; then
						log_message "ERROR" "❌ Failed to store passphrase for $keyname in secret store" "true"
					fi
				fi
			else
				# No GUI available: let ssh-add prompt on the terminal.
				log_message "INFO" "🖥️  No GUI detected, adding $keyname manually"
				ssh-add "$keyfile"
				ssh_add_result=$?
			fi

			if [ "$ssh_add_result" -eq 0 ]; then
				log_message "INFO" "✅ Added $keyname to agent" "true"
				break
			fi
			log_message "ERROR" "❌ Failed to add $keyname (attempt $((attempts + 1))/$max_attempts)" "true"
			attempts=$((attempts + 1))
		done
	done
fi
