# harnessator - build, test, man pages, packaging.
# Pure Go + system git; CGO not required anywhere (CGO_ENABLED=0).

MODULE  := harnessator.org/harnessator
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(MODULE)/internal/cli.Version=$(VERSION)
PREFIX  ?= $(HOME)/.local
DESTDIR ?=
GOFLAGS ?= -trimpath
BIN     := bin/harnessator
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build test vet fmt fmt-check cross dist man-check man install install-man uninstall clean drift

all: build test

build:
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/harnessator

test:
	go test -count=1 ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run 'make fmt'" >&2; exit 1; }

# documentation drift gate (rule 18-docs-sync, machine-enforced):
# every command + flag must appear in USAGE.md and the man pages.
drift:
	go test -count=1 -run TestDocs ./internal/cli/

# compile-verification matrix mirroring .gitlab-ci.yml (no test runs - the
# postponed runners own that; see BUILDING.md)
cross:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; \
		[ $$os = windows ] && ext=.exe; \
		echo "build $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -ldflags '$(LDFLAGS)' \
			-o dist/harnessator_$(VERSION)_$${os}_$${arch}$${ext} ./cmd/harnessator || exit 1; \
	done

dist: cross
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; tar_ext=tar.gz; \
		[ $$os = windows ] && ext=.exe && tar_ext=zip; \
		base=harnessator_$(VERSION)_$${os}_$${arch}; \
		if [ $$tar_ext = zip ]; then \
			(cd dist && zip -q $$base.zip $$base.exe); \
		else \
			cp LICENSE dist/ 2>/dev/null || true; \
			tar -czf dist/$$base.tar.gz -C dist $$base$${ext} $(shell [ -f LICENSE ] && echo LICENSE); \
			rm -f dist/$$base$${ext}; \
		fi; \
	done
	@sha256sum dist/harnessator_$(VERSION)_*.tar.gz dist/harnessator_$(VERSION)_*.zip 2>/dev/null \
		> dist/harnessator_$(VERSION)_checksums.txt || true
	@echo "dist/: $(shell ls dist/harnessator_$(VERSION)_* 2>/dev/null | xargs -n1 basename | tr '\n' ' ')"

# man pages: validate with groff - any diagnostic is a failure.
man-check:
	@fail=0; for f in man/man1/*.1 man/man5/*.5; do \
		err=$$(groff -mandoc -t -Tutf8 "$$f" 2>&1 >/dev/null); \
		if [ -n "$$err" ]; then echo "GROFF: $$f"; echo "$$err"; fail=1; fi; \
	done; [ $$fail -eq 0 ] && echo "man pages: groff clean" || exit 1

man: man-check
	@echo "preview with:  man man/man1/harnessator.1  (or groff -mandoc -Tutf8 ... | less -R)"

install: build install-man
	install -Dm755 $(BIN) $(DESTDIR)$(PREFIX)/bin/harnessator

install-man:
	@for f in man/man1/*.1 man/man5/*.5; do \
		d=$(DESTDIR)$(PREFIX)/share/man/$$(dirname $$f | basename); \
		install -Dm644 $$f $$d/$$(basename $$f) || exit 1; \
		gzip -9nf $$d/$$(basename $$f); \
	done
	@echo "man pages installed to $(PREFIX)/share/man"

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/harnessator
	rm -f $(DESTDIR)$(PREFIX)/share/man/man1/harnessator.1*
	rm -f $(DESTDIR)$(PREFIX)/share/man/man5/harnessator.*.5*

clean:
	rm -rf bin dist
