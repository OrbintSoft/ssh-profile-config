#!/bin/bash

# Get all ssh-agent PIDs for the current user
mapfile -t agent_pids < <(pgrep -u "$USER" -x ssh-agent)
key_dir="$HOME/.ssh"
ssh_add_sessions_file="$key_dir/ssh_add.sessions"
log_file="$key_dir/sessions.log"
key_prefix="SSH-Key"
ssh_askpass_script="/usr/local/bin/ssh-ask-pass.sh"
max_log_lines=100
max_attempts=3

# Function to find latest valid SSH_AUTH_SOCK
find_latest_ssh_sock() {
	find "$HOME/.ssh/agent" -type s 2>/dev/null | head -n 1 && return 0
    find "/tmp" -type s -user "$USER" -name 'agent.*' -print0 2>/dev/null | xargs -0 -r ls -1t 2>/dev/null | head -n 1
}

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
   	echo "$log_message" >> "$log_file"
	if [ "$print_message" = true ]; then
		echo "$log_message"
	fi
   	line_count=$(wc -l < "$log_file")
	if [ "$line_count" -gt "$max_log_lines" ]; then
        file_cut=$(tail -n "$max_log_lines" "$log_file")
		echo "$file_cut" > "$log_file"
	fi
}

# Warn if multiple agents found
if [ "${#agent_pids[@]}" -gt 1 ]; then
    log_message "WARNING" "Multiple ssh-agent processes detected for user $USER:" "true"
    for pid in "${agent_pids[@]}"; do
        echo " - PID $pid: $(ps -p "$pid" -o args=)" >&2
    done
fi


# if there are agents running, check SSH_AUTH_SOCK and SSH_AGENT_PID
if [ "${#agent_pids[@]}" -gt 0 ]; then
    log_message "INFO" "Found ${#agent_pids[@]} ssh-agent processes for user $USER"
	# Check if SSH_AUTH_SOCK is set and valid
    if [ -n "$SSH_AUTH_SOCK" ]; then
        if [ ! -S "$SSH_AUTH_SOCK" ]; then
            log_message "WARNING" "SSH_AUTH_SOCK is set but invalid: $SSH_AUTH_SOCK" "true"
            export SSH_AUTH_SOCK=""
        fi
    fi
	# Check if SSH_AUTH_SOCK is set, if not, find the latest valid socket
    if [ -z "$SSH_AUTH_SOCK" ]; then
        new_sock=$(find_latest_ssh_sock)
        if [ -n "$new_sock" ]; then
            export SSH_AUTH_SOCK="$new_sock"
            log_message "INFO" "Set SSH_AUTH_SOCK=$SSH_AUTH_SOCK" "true"
        else
            log_message "ERROR" "Could not find a valid ssh-agent socket." "true"
        fi
    fi

    # Check if SSH_AGENT_PID is set and valid
    pid_valid=false
    if [ -n "$SSH_AGENT_PID" ]; then
        for pid in "${agent_pids[@]}"; do
            if [ "$pid" = "$SSH_AGENT_PID" ]; then
                pid_valid=true
                break
            fi
        done
        if [ "$pid_valid" = false ]; then
            log_message "WARNING" "SSH_AGENT_PID $SSH_AGENT_PID is not among known agent PIDs." "true"
            export SSH_AGENT_PID=""
        fi
    fi
	# If SSH_AGENT_PID is not set, use the highest PID from the agent PIDs
    if [ -z "$SSH_AGENT_PID" ]; then
        highest_pid=$(printf "%s\n" "${agent_pids[@]}" | sort -nr | head -n 1)
        export SSH_AGENT_PID="$highest_pid"
        log_message "INFO" "Set SSH_AGENT_PID=$SSH_AGENT_PID" "true"
    fi

else
    # No ssh-agent running, start a new one
    log_message "INFO" "No ssh-agent running. Starting a new one..."
    eval "$(ssh-agent -s)" > /dev/null
    log_message "INFO" "New ssh-agent started with PID $SSH_AGENT_PID and socket $SSH_AUTH_SOCK"
fi

gui_available=false
if [ -n "$DISPLAY" ] && command -v xset &>/dev/null && xset q &>/dev/null; then
    gui_available=true
fi

# ssh-add only if interactive
if [[ $- == *i* ]]; then
	# Check if the sessions file exists, create it if not
	touch "$ssh_add_sessions_file"
	chmod 600 "$ssh_add_sessions_file"
	# search for existing keys in the .ssh directory
	find "$key_dir" -type f -name 'id_*' ! -name '*.pub' | while read -r keyfile; do
		keyname=$(basename "$keyfile")
        ssh_add_result=false
		# Check if the key is already added to the agent
		if grep -Fxq "$SSH_AGENT_PID|$SSH_AUTH_SOCK|$keyfile" "$ssh_add_sessions_file"; then
	        log_message "INFO" "✅ $keyname already added to agent $SSH_AGENT_PID"
        else
            attempts=0
			# Try to add the key with a maximum number of attempts
            while [ $attempts -lt $max_attempts ]; do
                wallet_key="$key_prefix-$keyname"
                if [ "$gui_available" = true ]; then
					# Lookup the passphrase in the secret store
                    passphrase=$(secret-tool lookup service "$wallet_key" username "$USER")
                    passphrase_stored=false
                    if [[ -n "$passphrase" && ! "$passphrase" =~ ^[[:space:]]*$ ]]; then
                        log_message "INFO" "🔐 Using stored passphrase for $keyname"
                        passphrase_stored=true
                    else
						# If passphrase is not found, prompt the user
                        log_message "INFO" "❓ No stored entry for $keyname, prompting..." "true"
                        if ! passphrase=$(kdialog --password "Enter passphrase for $keyname"); then
    						log_message "ERROR" "❌ kdialog failed to get passphrase for $keyname" "true"
    						break
						fi
                    fi
					# Create a temporary keyctl entry for the passphrase
                    ssh_pass_uuid="$(uuidgen | tr -d '-')"
                    if ! ssh_tmp_ketctl=$(keyctl add user "$ssh_pass_uuid" "$passphrase" @u); then
                        log_message "ERROR" "❌ Failed to add keyctl for $keyname" "true"
                        break
                    fi
                    if ! keyctl timeout "$ssh_tmp_ketctl" 60 > /dev/null ; then
						log_message "ERROR" "❌ Failed to set keyctl timeout for $keyname"
						break
					fi
					# Add the key using ssh-add with the temporary keyctl
                    SSH_TEMP_KEYCTL="$ssh_tmp_ketctl" SSH_PASS_UUID="$ssh_pass_uuid" SSH_ASKPASS="$ssh_askpass_script" setsid ssh-add "$keyfile" < /dev/null
                    ssh_add_result="$?"
					# If the key was added successfully, store the passphrase in the secret store
                    if [ "$passphrase_stored" = false ] && [ "$ssh_add_result" -eq 0 ]; then
						if ! echo "$passphrase" | secret-tool store --label="SSH Passphrase for $keyname" service "$wallet_key" username "$USER"; then
							log_message "ERROR" "❌ Failed to store passphrase for $keyname in secret store" "true"
							break
						fi
                    fi
                else
					# If no GUI is available, use ssh-add directly
                    log_message "INFO" "🖥️  No GUI detected, adding $keyname manually"
                    ssh-add "$keyname"
                    ssh_add_result="$?"
                fi
				# If the key was added successfully, store the session
                if [ $ssh_add_result -eq 0 ]; then
                    echo "$SSH_AGENT_PID|$SSH_AUTH_SOCK|$keyfile" >> "$ssh_add_sessions_file"
                    log_message "INFO" "✅ Added $keyname to agent $SSH_AGENT_PID" "true"
                    break
                else
                    log_message "ERROR" "❌ Failed to add $keyname (attempt $((attempts+1))/$max_attempts)" "true"
                fi
                attempts=$((attempts+1))
            done
	    fi
	done
fi
