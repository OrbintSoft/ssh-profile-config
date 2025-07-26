UNAME := $(shell uname)

ifeq ($(UNAME),Linux)
INSTALL_CMDS = \
	echo "Installing ssh-ask-pass.sh" && \
	cp -f ssh-ask-pass-linux.sh $(DESTDIR)/usr/local/bin/ssh-ask-pass.sh && \
	chmod +x $(DESTDIR)/usr/local/bin/ssh-ask-pass.sh && \
	echo "Installation complete."

UNINSTALL_CMDS = \
	echo "Uninstalling ssh-ask-pass.sh" && \
	rm -f $(DESTDIR)/usr/local/bin/ssh-ask-pass.sh && \
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
