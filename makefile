UNAME := $(shell uname)

ifeq ($(UNAME),Linux)
INSTALL_CMDS = \
	echo "Installing ssh-ask-pass.sh" && \
	sudo cp -f ssh-ask-pass-linux.sh /usr/local/bin/ssh-ask-pass.sh && \
	sudo chmod +x /usr/local/bin/ssh-ask-pass.git push originmastersh && \
	echo "Installation complete."

UNINSTALL_CMDS = \
	echo "Uninstalling ssh-ask-pass.sh" && \
	sudo rm -f /usr/local/bin/ssh-ask-pass.sh && \
	echo "Uninstallation complete."
else
INSTALL_CMDS = \
	echo "$(UNAME) is not supported." && \
	exit 1

UNINSTALL_CMDS = \
	echo "$(UNAME) is not supported." && \
	exit 1
endif

install:
	@sh -c '$(INSTALL_CMDS)'

uninstall:
	@sh -c '$(UNINSTALL_CMDS)'

.PHONY: install uninstall
.DEFAULT_GOAL := install
