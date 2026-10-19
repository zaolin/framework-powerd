.PHONY: build test test-cover test-python vet lint clean install install-binary install-config install-secret install-systemd enable disable uninstall token all

BINARY := framework-powerd
BUILD_DIR := cmd/framework-powerd
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

SUDO ?= sudo
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
SYSTEMD_DIR ?= /etc/systemd/system
CONFIG_DIR ?= /etc/framework-powerd
UNIT_NAME := framework-powerd.service
UNIT_PATH := $(SYSTEMD_DIR)/$(UNIT_NAME)
ENV_FILE := $(CONFIG_DIR)/env
CONFIG_FILE := $(CONFIG_DIR)/config.json

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./$(BUILD_DIR)

test:
	go test -race ./...

test-cover:
	go test -race -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1

test-python:
	python -m pytest custom_components/framework_powerd/tests/ -q

vet:
	go vet ./...

lint: vet
	go build ./...

clean:
	rm -f $(BINARY) coverage.out

# Full local installation: binary, example config (if absent), JWT secret (if
# absent) and the systemd unit. Run `make enable` afterwards to start it.
install: install-binary install-config install-secret install-systemd
	@echo
	@echo "Installed $(BINARY) to $(BINDIR)"
	@echo "Installed unit to $(UNIT_PATH)"
	@echo "Config directory: $(CONFIG_DIR)"
	@echo
	@echo "Generate a Home Assistant token with: make token"
	@echo "Enable and start the service with:    make enable"

install-binary: build
	$(SUDO) install -Dm755 $(BINARY) $(BINDIR)/$(BINARY)

install-config:
	$(SUDO) install -d -m755 $(CONFIG_DIR)
	@if [ ! -f $(CONFIG_FILE) ]; then \
		$(SUDO) install -m644 configs/config.example.json $(CONFIG_FILE); \
		echo "Installed example config to $(CONFIG_FILE)"; \
	else \
		echo "Keeping existing $(CONFIG_FILE)"; \
	fi

install-secret:
	$(SUDO) install -d -m700 $(CONFIG_DIR)
	@if [ -s $(ENV_FILE) ]; then \
		echo "Keeping existing JWT secret in $(ENV_FILE)"; \
	elif $(SUDO) grep -Eq '"jwt_secret"[[:space:]]*:[[:space:]]*"[^"]+"' $(CONFIG_FILE) 2>/dev/null; then \
		echo "Using jwt_secret from $(CONFIG_FILE)"; \
	else \
		secret=$$(openssl rand -hex 32 2>/dev/null || od -An -N32 -tx1 /dev/urandom | tr -d ' \n'); \
		printf 'JWT_SECRET=%s\n' "$$secret" | $(SUDO) tee $(ENV_FILE) >/dev/null; \
		$(SUDO) chmod 600 $(ENV_FILE); \
		echo "Generated JWT secret in $(ENV_FILE)"; \
	fi

install-systemd:
	$(SUDO) install -Dm644 configs/systemd/$(UNIT_NAME) $(UNIT_PATH)
	$(SUDO) systemctl daemon-reload

enable:
	$(SUDO) systemctl enable --now $(UNIT_NAME)
	$(SUDO) systemctl --no-pager --full status $(UNIT_NAME) || true

disable:
	-$(SUDO) systemctl disable --now $(UNIT_NAME)

uninstall: disable
	-$(SUDO) rm -f $(UNIT_PATH)
	$(SUDO) systemctl daemon-reload
	-$(SUDO) rm -f $(BINDIR)/$(BINARY)
	@echo "Removed $(BINDIR)/$(BINARY) and $(UNIT_PATH)"
	@echo "Kept $(CONFIG_DIR) (config and JWT secret)"

# Print a JWT token for Home Assistant using the installed secret.
token:
	@secret=$$($(SUDO) sed -n 's/^JWT_SECRET=//p' $(ENV_FILE) 2>/dev/null); \
	$(SUDO) env JWT_SECRET="$$secret" $(BINDIR)/$(BINARY) token
