# Builds and installs ct: ./configure && make && make install
# (make works without configure too, with the defaults below). Targets:
#   all (build)  ct, stamped with the version from git describe
#   test         go test ./...
#   check        go vet, gofmt and the tests
#   install      ct and its zsh/bash/fish completions (DESTDIR honoured)
#   uninstall    removes them
#   cross        ct for linux/darwin/windows x amd64/arm64 into dist/
#   clean        removes ct, dist/ and config.mk
-include config.mk

PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DATADIR ?= $(PREFIX)/share
GO ?= go

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -s -w -X github.com/zero4573/claude-tickets/internal/version.Version=$(VERSION)
PLATFORMS = linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

ZSHDIR = $(DATADIR)/zsh/site-functions
BASHDIR = $(DATADIR)/bash-completion/completions
FISHDIR = $(DATADIR)/fish/vendor_completions.d

.PHONY: all build test check install uninstall cross clean

all: build

build: ct

# A file target, so `sudo make install` after `make` doesn't rebuild as root
ct: go.mod go.sum $(shell find cmd internal assets -type f)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o ct ./cmd/ct

test:
	$(GO) test ./...

check:
	$(GO) vet ./...
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) test ./...

install: ct
	install -d '$(DESTDIR)$(BINDIR)' '$(DESTDIR)$(ZSHDIR)' '$(DESTDIR)$(BASHDIR)' '$(DESTDIR)$(FISHDIR)'
	install -m 0755 ct '$(DESTDIR)$(BINDIR)/ct'
	./ct completion zsh > '$(DESTDIR)$(ZSHDIR)/_ct'
	./ct completion bash > '$(DESTDIR)$(BASHDIR)/ct'
	./ct completion fish > '$(DESTDIR)$(FISHDIR)/ct.fish'
	@echo "installed ct $(VERSION) into $(DESTDIR)$(BINDIR); next: ct vault init <name>"

uninstall:
	rm -f '$(DESTDIR)$(BINDIR)/ct' '$(DESTDIR)$(ZSHDIR)/_ct' '$(DESTDIR)$(BASHDIR)/ct' '$(DESTDIR)$(FISHDIR)/ct.fish'

cross:
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
	  echo "dist/ct-$$os-$$arch$$ext"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/ct-$$os-$$arch$$ext ./cmd/ct || exit 1; \
	done

clean:
	rm -rf ct dist config.mk
