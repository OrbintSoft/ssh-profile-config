#!/bin/bash

key_dir="$HOME/.ssh"
log_file="$key_dir/sessions.log"

log_message() {
	local level="$1"
	local message="$2"
    local datetime
	datetime=$(date '+%Y-%m-%d %H:%M:%S')
	echo "$datetime | [$level] $message" >> "$log_file"
}

log_message "INFO" "🔑 ssh-ask-pass.sh started for user $USER"

if [ -n "$SSH_TEMP_KEYCTL" ]; then
	if ! passphrase="$(keyctl print "$SSH_TEMP_KEYCTL")"; then
		log_message "ERROR" "❗ Failed to retrieve passphrase for SSH_TEMP_KEYCTL: ***${SSH_TEMP_KEYCTL: -4} UUID: ***${SSH_PASS_UUID: -8}"
		exit 1
	else
		log_message "INFO" "🔑 Successfully retrieved passphrase for SSH_TEMP_KEYCTL: ***${SSH_TEMP_KEYCTL: -3} UUID: ${SSH_PASS_UUID: -3}"
	fi
	if [ -n "$passphrase" ]; then
		if ! keyctl unlink "$SSH_TEMP_KEYCTL" > /dev/null; then
			log_message "ERROR" "❗ Failed to unlink SSH_TEMP_KEYCTL: ${SSH_TEMP_KEYCTL: -3} UUID: ${SSH_PASS_UUID: -3}"
			exit 1
		fi
		echo "$passphrase"
	else
		log_message "ERROR" "❗ Failed to retrieve passphrase for SSH_TEMP_KEYCTL:${SSH_TEMP_KEYCTL: -4} ssh_pass_uuid: ${SSH_PASS_UUID: -6}"
		exit 1
	fi
else
	log_message "ERROR" "❗ SSH_TEMP_KEYCTL not set, cannot retrieve passphrase."
	exit 1
fi
