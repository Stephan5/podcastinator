BINARY    := podcastinator
BUILD_DIR := build
MAIN      := ./cmd/podcastinator

.DEFAULT_GOAL := build

.PHONY: build
build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) $(MAIN)

.PHONY: build-linux build-darwin-arm64 build-darwin-amd64
build-linux:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY)-linux-amd64 $(MAIN)

build-darwin-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 $(MAIN)

build-darwin-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 $(MAIN)

.PHONY: test
test:
	go test -v -count=1 ./internal/... ./cmd/podcastinator/...

.PHONY: test-integration
test-integration: build
	go test -v -count=1 ./test/...

.PHONY: test-all
test-all: test test-integration

.PHONY: test-short
test-short:
	go test -count=1 ./internal/... ./cmd/podcastinator/...

.PHONY: test-race
test-race:
	go test -race -count=1 ./internal/... ./cmd/podcastinator/...

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint: fmt vet

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

VERSION := $(shell sed -n 's/^const version = "\(.*\)"$$/\1/p' cmd/podcastinator/version.go)

# Prepares release V (make cut V=0.10.0) on master: sets the version in
# version.go, points the README's go install lines at it and heads the release
# notes' Unreleased section with it, then commits that as "cut vV" and shows
# it. Push it, then run make release.
.PHONY: cut
cut:
	@echo "$(V)" | grep -qxE '[0-9]+\.[0-9]+\.[0-9]+' || { echo "Usage: make cut V=<major>.<minor>.<patch>" >&2; exit 1; }
	@awk -v old="$(VERSION)" -v new="$(V)" 'BEGIN { split(old, o, "."); split(new, n, "."); for (i = 1; i <= 3; i++) if (n[i] != o[i]) exit !(n[i] + 0 > o[i] + 0); exit 1 }' || { echo "$(V) isn't newer than $(VERSION)" >&2; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "Commit or stash your changes first" >&2; exit 1; }
	@test "$$(git rev-parse --abbrev-ref HEAD)" = master || { echo "Cut from master" >&2; exit 1; }
	@git fetch -q origin master --tags
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/master)" || { echo "master doesn't match origin/master; pull or push first" >&2; exit 1; }
	@! git rev-parse -q --verify "refs/tags/v$(V)" >/dev/null || { echo "v$(V) is already tagged" >&2; exit 1; }
	@awk '$$0 == "## Unreleased" {found = 1; next} found && /^## / {exit} found' RELEASE_NOTES.md | grep -q '[^[:space:]]' || { echo "RELEASE_NOTES.md has no notes under '## Unreleased'" >&2; exit 1; }
	sed -i.bak 's/^const version = ".*"$$/const version = "$(V)"/' cmd/podcastinator/version.go
	sed -i.bak 's#podcastinator/cmd/podcastinator@v[0-9][0-9.]*#podcastinator/cmd/podcastinator@v$(V)#' README.md
	sed -i.bak 's/^## Unreleased$$/## $(V)/' RELEASE_NOTES.md
	@rm cmd/podcastinator/version.go.bak README.md.bak RELEASE_NOTES.md.bak
	git commit -q -m "cut v$(V)" cmd/podcastinator/version.go README.md RELEASE_NOTES.md
	git show

# Tags master with the version in version.go and pushes the tag, which runs the
# release workflow. It first checks what the workflow would refuse, so a bad tag
# never reaches GitHub.
.PHONY: release
release:
	@test -n "$(VERSION)" || { echo "No version in cmd/podcastinator/version.go" >&2; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "Commit or stash your changes first" >&2; exit 1; }
	@test "$$(git rev-parse --abbrev-ref HEAD)" = master || { echo "Release from master" >&2; exit 1; }
	@git fetch -q origin master --tags
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/master)" || { echo "master doesn't match origin/master; pull or push first" >&2; exit 1; }
	@! git rev-parse -q --verify "refs/tags/v$(VERSION)" >/dev/null || { echo "v$(VERSION) is already tagged; bump the version in cmd/podcastinator/version.go" >&2; exit 1; }
	@grep -qxF '## $(VERSION)' RELEASE_NOTES.md || { echo "RELEASE_NOTES.md has no '## $(VERSION)' section" >&2; exit 1; }
	git tag v$(VERSION)
	git push origin v$(VERSION)

.PHONY: help
help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Build:"
	@echo "  build              Build binary to $(BUILD_DIR)/$(BINARY)"
	@echo "  build-linux        Cross-compile for linux/amd64"
	@echo "  build-darwin-arm64 Cross-compile for darwin/arm64"
	@echo "  build-darwin-amd64 Cross-compile for darwin/amd64"
	@echo ""
	@echo "Test:"
	@echo "  test               Run unit tests (internal/... cmd/podcastinator/...) with verbose output"
	@echo "  test-integration   Build binary then run integration tests in test/"
	@echo "  test-all           Run unit tests + integration tests"
	@echo "  test-short         Run unit tests (quiet)"
	@echo "  test-race          Run unit tests with race detector"
	@echo ""
	@echo "Code quality:"
	@echo "  fmt                Run go fmt"
	@echo "  vet                Run go vet"
	@echo "  lint               Run fmt + vet"
	@echo ""
	@echo "Release:"
	@echo "  cut V=x.y.z        Set the version, README and release notes for V, commit and show it"
	@echo "  release            Tag master with the version in version.go and push the tag"
	@echo ""
	@echo "Other:"
	@echo "  tidy               Run go mod tidy"
	@echo "  clean              Remove $(BUILD_DIR)/"

