UNAME := $(shell uname)

PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DESTDIR ?=
ETC_PROFILE_D ?= /etc/profile.d/
NN ?= 001

ifeq ($(UNAME),Linux)
SSH_ASK_INSTALL_SCRIPT = ssh-ask-pass-linux.sh
SSH_INIT_INSTALL_SCRIPT = nn-ssh-init-linux.sh
INSTALL_PATH = $(DESTDIR)$(BINDIR)
SSH_ASK_INSTALL_PATH = $(INSTALL_PATH)/ssh-ask-pass.sh
SSH_INIT_NAME= $(NN)-ssh-init.sh
SSH_INIT_BIND_PATH = $(ETC_PROFILE_D)$(NN)-ssh-init.sh
SSH_INIT_INSTALL_PATH = $(DESTDIR)$(SSH_INIT_BIND_PATH)

install:
	@echo "Installing $(SSH_ASK_INSTALL_SCRIPT) to $(SSH_ASK_INSTALL_PATH)"
	@install -Dm755 $(SSH_ASK_INSTALL_SCRIPT) $(SSH_ASK_INSTALL_PATH)
	@echo "Installing $(SSH_INIT_INSTALL_SCRIPT) to $(SSH_INIT_INSTALL_PATH)"
	@install -Dm755 $(SSH_INIT_INSTALL_SCRIPT) $(SSH_INIT_INSTALL_PATH)
	@echo "Set ssh-ask-pass script path in $(SSH_INIT_INSTALL_PATH)"
	@sed -i 's|/usr/local/bin/ssh-ask-pass\.sh|$(SSH_INIT_BIND_PATH)|g' $(SSH_INIT_INSTALL_PATH)
	@echo "Installation complete."

uninstall:
	@echo "Uninstalling $(SSH_ASK_INSTALL_PATH)"
	@rm -f $(SSH_ASK_INSTALL_PATH)
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
	@echo "SSH_ASK_INSTALL_PATH: $(SSH_ASK_INSTALL_PATH)"

.PHONY: install uninstall print-paths
.DEFAULT_GOAL := install
