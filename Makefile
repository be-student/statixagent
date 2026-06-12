# StatixAgent build entry points. Release builds happen in CI (.github/
# workflows/release.yml); this Makefile mirrors them for local use.
VERSION ?= dev
PUBKEY  ?=
LDFLAGS  = -s -w -X main.version=$(VERSION) -X main.pubKeyHex=$(PUBKEY)

.PHONY: test vet build dist clean

test:
	go test ./...

vet:
	go vet ./...

build: vet
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/statix-agent ./cmd/statix-agent
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o bin/statix-install ./cmd/statix-install

# dist builds every release artifact + checksums (signing happens in CI).
dist: test
	rm -rf dist && mkdir -p dist
	for arch in amd64 arm64; do \
		GOOS=linux GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/statix-agent_linux_$$arch ./cmd/statix-agent; \
		GOOS=linux GOARCH=$$arch go build -trimpath -ldflags "-s -w" -o dist/statix-install_linux_$$arch ./cmd/statix-install; \
	done
	cd dist && sha256sum * > checksums.txt

clean:
	rm -rf bin dist
