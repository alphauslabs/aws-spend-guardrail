VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build guardrail guard test fmt vet clean

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
