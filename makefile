UNAME := $(shell uname)

ifeq ($(UNAME),Linux)
INSTALL_CMDS = \
	echo "Installing ssh-ask-pass.sh"; \
	cp -f ssh-ask-pass-linux.sh /usr/local/bin/ssh-ask-pass; \
	chmod +x /usr/local/bin/ssh-ask-pass; \
	echo "Installation complete."

UNINSTALL_CMDS = \
	echo "Uninstalling ssh-ask-pass.sh"; \
	rm -f /usr/local/bin/ssh-ask-pass; \
	echo "Uninstallation complete."
else
INSTALL_CMDS = \
	echo "$(UNAME) is not supported."; \
	exit 1

UNINSTALL_CMDS = \
	echo "$(UNAME) is not supported."; \
	exit 1
endif

install:
	$(INSTALL_CMDS)

uninstall:
	$(UNINSTALL_CMDS)

.PHONY: install uninstall
.DEFAULT_GOAL := install
