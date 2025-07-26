UNAME := $(shell uname)

PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DESTDIR ?=

ifeq ($(UNAME),Linux)
INSTALL_SCRIPT = ssh-ask-pass-linux.sh
INSTALL_PATH = $(DESTDIR)$(BINDIR)/ssh-ask-pass.sh

install:
	@echo "Installing to $(INSTALL_PATH)"
	@install -Dm755 $(INSTALL_SCRIPT) $(INSTALL_PATH)
	@echo "Installation complete."

uninstall:
	@echo "Uninstalling $(INSTALL_PATH)"
	@rm -f $(INSTALL_PATH)
	@echo "Uninstallation complete."

else

install uninstall:
	@echo "$(UNAME) is not supported."
	@exit 1

endif

print-paths:
	@echo "PREFIX: $(PREFIX)"
	@echo "BINDIR: $(BINDIR)"
	@echo "DESTDIR: $(DESTDIR)"
	@echo "INSTALL_PATH: $(INSTALL_PATH)"


.PHONY: install uninstall print-paths
.DEFAULT_GOAL := install
