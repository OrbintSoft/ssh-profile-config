UNAME := $(shell uname)

PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DESTDIR ?=
ETC_PROFILE_D ?= /etc/profile.d/
NN ?= 001

GO ?= go
GO_MAIN = ./cmd/sshakku
GO_BIN = bin/sshakku

ifeq ($(UNAME),Linux)
SSH_INIT_INSTALL_SCRIPT = nn-ssh-init-linux.sh
INSTALL_PATH = $(DESTDIR)$(BINDIR)
SSH_INIT_NAME= $(NN)-ssh-init.sh
SSH_INIT_BIND_PATH = $(ETC_PROFILE_D)$(SSH_INIT_NAME)
SSH_INIT_INSTALL_PATH = $(DESTDIR)$(SSH_INIT_BIND_PATH)
SSHAKKU_INSTALL_PATH = $(INSTALL_PATH)/sshakku
SSHAKKU_RUNTIME_PATH = $(BINDIR)/sshakku

install: build
	@echo "Installing $(GO_BIN) to $(SSHAKKU_INSTALL_PATH)"
	@install -Dm755 $(GO_BIN) $(SSHAKKU_INSTALL_PATH)
	@echo "Installing $(SSH_INIT_INSTALL_SCRIPT) to $(SSH_INIT_INSTALL_PATH)"
	@install -Dm755 $(SSH_INIT_INSTALL_SCRIPT) $(SSH_INIT_INSTALL_PATH)
	@echo "Setting binary paths in $(SSH_INIT_INSTALL_PATH)"
	@sed -i 's|/usr/local/bin/sshakku|$(SSHAKKU_RUNTIME_PATH)|g' $(SSH_INIT_INSTALL_PATH)
	@echo "Installation complete."

uninstall:
	@echo "Uninstalling $(SSHAKKU_INSTALL_PATH)"
	@rm -f $(SSHAKKU_INSTALL_PATH)
	@echo "Uninstalling $(SSH_INIT_INSTALL_PATH)"
	@rm -f $(SSH_INIT_INSTALL_PATH)
	@echo "Uninstallation complete."

else

install uninstall:
	@echo "$(UNAME) is not supported."
	@exit 1
endif

build:
	$(GO) build -o $(GO_BIN) $(GO_MAIN)

print-paths:
	@echo "PREFIX: $(PREFIX)"
	@echo "BINDIR: $(BINDIR)"
	@echo "DESTDIR: $(DESTDIR)"
	@echo "SSHAKKU_INSTALL_PATH: $(SSHAKKU_INSTALL_PATH)"
	@echo "SSHAKKU_RUNTIME_PATH: $(SSHAKKU_RUNTIME_PATH)"
	@echo "SSH_INIT_INSTALL_PATH: $(SSH_INIT_INSTALL_PATH)"

# Linting. Requires: shellcheck, shfmt, markdownlint-cli2, taplo, checkmake,
# actionlint, editorconfig-checker. Each tool reads its own config file where it
# has one.
SH_SCRIPTS = $(wildcard *.sh) $(wildcard .githooks/*)

lint: lint-sh lint-md lint-toml lint-make lint-yaml lint-editorconfig lint-go

lint-sh:
	shellcheck $(SH_SCRIPTS)
	shfmt -d $(SH_SCRIPTS)

lint-md:
	markdownlint-cli2

lint-toml:
	taplo lint
	taplo format --check

lint-make:
	checkmake --config=checkmake.ini Makefile

lint-yaml:
	actionlint

lint-editorconfig:
	editorconfig-checker

lint-go:
	@gofmt_out=$$(gofmt -l .); [ -z "$$gofmt_out" ] || { echo "gofmt needed on:"; echo "$$gofmt_out"; exit 1; }
	$(GO) vet ./...
	golangci-lint run

.PHONY: install uninstall build print-paths lint lint-sh lint-md lint-toml lint-make lint-yaml lint-editorconfig lint-go
.DEFAULT_GOAL := install
