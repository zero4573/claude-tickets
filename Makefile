# Builds and installs tix: ./configure && make && make install
# (make works without configure too, with the defaults below). Targets:
#   all (build)  tix, stamped with the version from git describe
#   test         go test ./...
#   bench        vault search token/latency benchmark (internal/vbench)
#   check        go vet, gofmt and the tests
#   install      tix and its zsh/bash/fish completions (DESTDIR honoured)
#   uninstall    removes them
#   cross        tix for linux/darwin/windows x amd64/arm64 into dist/
#   clean        removes tix, dist/ and config.mk
-include config.mk

PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DATADIR ?= $(PREFIX)/share
GO ?= go

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -s -w -X github.com/zero4573/tix/internal/version.Version=$(VERSION)
PLATFORMS = linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

ZSHDIR = $(DATADIR)/zsh/site-functions
BASHDIR = $(DATADIR)/bash-completion/completions
FISHDIR = $(DATADIR)/fish/vendor_completions.d

.PHONY: all build test bench check install uninstall cross clean

all: build

build: tix

# A file target, so `sudo make install` after `make` doesn't rebuild as root
tix: go.mod go.sum $(shell find cmd internal assets -type f)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o tix ./cmd/tix

test:
	$(GO) test ./...

# Token/latency benchmark on a generated 2,000-note vault. The default stub embedder makes the
# numbers a regression guard (budget, latency, keyword-only quality floor), not real-model numbers:
# for those run `tix vault model fetch` on the host first, then `TIX_EMBED_REAL=1 make bench`.
bench:
	TIX_BENCH=1 $(GO) test ./internal/vbench -run TestReport -v -timeout 30m

# go test runs one process per package, each with many threads; unbounded on a 24-CPU box under
# the sandbox pids limit (pids.max 2048) it can hit pthread_create/fork "resource temporarily
# unavailable". -p 4 stays well under it. Override: make check TESTFLAGS="-p 8"
TESTFLAGS ?= -p 4

# make check TESTLOG=<file> also writes the tests' `go test -json` output to <file>, keeping
# the plain output on the terminal, so a failure's log survives the rerun.
TESTLOG ?=

check:
	$(GO) vet ./...
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
ifeq ($(TESTLOG),)
	$(GO) test $(TESTFLAGS) ./...
else
	@# one run: the json goes to the file, jq prints its output lines (the plain text). jq is
	@# required: without it the pipe would die of SIGPIPE and leave the log empty, so fail first.
	@command -v jq >/dev/null 2>&1 || { echo "make check TESTLOG needs jq on PATH; run make check without TESTLOG" >&2; exit 1; }
	@rc=$$(mktemp); { $(GO) test $(TESTFLAGS) -json ./... ; echo $$? > $$rc; } | tee '$(TESTLOG)' | jq -j 'select(.Action=="output" or .Action=="build-output").Output // empty'; \
	  r=$$(cat $$rc); rm -f $$rc; exit $$r
endif

install: tix
	install -d '$(DESTDIR)$(BINDIR)' '$(DESTDIR)$(ZSHDIR)' '$(DESTDIR)$(BASHDIR)' '$(DESTDIR)$(FISHDIR)'
	install -m 0755 tix '$(DESTDIR)$(BINDIR)/tix'
	./tix completion zsh > '$(DESTDIR)$(ZSHDIR)/_tix'
	./tix completion bash > '$(DESTDIR)$(BASHDIR)/tix'
	./tix completion fish > '$(DESTDIR)$(FISHDIR)/tix.fish'
	@echo "installed tix $(VERSION) into $(DESTDIR)$(BINDIR); next: tix vault init <name>"

uninstall:
	rm -f '$(DESTDIR)$(BINDIR)/tix' '$(DESTDIR)$(ZSHDIR)/_tix' '$(DESTDIR)$(BASHDIR)/tix' '$(DESTDIR)$(FISHDIR)/tix.fish'

cross:
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
	  echo "dist/tix-$$os-$$arch$$ext"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/tix-$$os-$$arch$$ext ./cmd/tix || exit 1; \
	done

clean:
	rm -rf tix dist config.mk
