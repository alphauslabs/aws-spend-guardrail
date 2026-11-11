# The Go toolchain is pinned so a release tag always builds with the same
# compiler; keep it equal to the toolchain line in go.mod.
export GOTOOLCHAIN := go1.27.4

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build guardrail guard test fmt vet clean go-version

all: build

build: guardrail guard

# The operator CLI, for this machine.
guardrail:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/guardrail ./cmd/guardrail

# The guard function for AWS Lambda (provided.al2023, arm64), and its zip.
guard:
	mkdir -p bin/guard
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -tags lambda.norpc -ldflags "$(LDFLAGS)" -o bin/guard/bootstrap ./guard
	cd bin/guard && rm -f ../guard.zip && python3 -m zipfile -c ../guard.zip bootstrap

test:
	go test ./...

fmt:
	goimports -w $$(git ls-files '*.go' 2>/dev/null || find . -name '*.go')

vet:
	go vet ./...

clean:
	rm -rf bin

go-version:
	@go version

# ---- Releases -------------------------------------------------------------
#
# make release VERSION=vX.Y.Z builds every release asset into dist/ and writes
# dist/SHA256SUMS. Anyone building the same tag with this target gets the
# same bytes: the toolchain is pinned, paths and build ids are stripped, and
# the function zip is written by tools/zipdet with the tag commit's time.

DIST := dist
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct)
RELEASE_FLAGS := -trimpath -buildvcs=false -ldflags "-buildid= -s -w -X main.version=$(VERSION)"
CLI_PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: release release-check sums

release-check:
	@test -n "$(VERSION)" || { echo "set VERSION, e.g. make release VERSION=v0.1.0"; exit 1; }

release: release-check
	rm -rf $(DIST) && mkdir -p $(DIST)/build
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(RELEASE_FLAGS) -tags lambda.norpc -o $(DIST)/build/bootstrap ./guard
	go run ./tools/zipdet -mtime $(SOURCE_DATE_EPOCH) -o $(DIST)/guard-$(VERSION)-linux-arm64.zip $(DIST)/build/bootstrap
	for p in $(CLI_PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(RELEASE_FLAGS) -o $(DIST)/guardrail-$(VERSION)-$$os-$$arch ./cmd/guardrail || exit 1; \
	done
	cp guard/template.yaml $(DIST)/template.yaml
	rm -rf $(DIST)/build
	$(MAKE) --no-print-directory sums

# SHA256SUMS covers every asset in dist/ except itself and its signature.
sums:
	cd $(DIST) && rm -f SHA256SUMS && find . -maxdepth 1 -type f ! -name 'SHA256SUMS*' -printf '%f\n' | LC_ALL=C sort | xargs sha256sum > SHA256SUMS
	@cat $(DIST)/SHA256SUMS
