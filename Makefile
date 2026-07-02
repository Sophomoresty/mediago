BINARY = mediago
MODULE = github.com/Sophomoresty/mediago
VERSION = $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: build build-all clean test release snapshot

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/mediago

build-all:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY).exe ./cmd/mediago
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 ./cmd/mediago
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 ./cmd/mediago
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-amd64 ./cmd/mediago
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64 ./cmd/mediago

test:
	go test ./...

clean:
	rm -f $(BINARY)
	rm -rf dist/

release:
	goreleaser release --clean

snapshot:
	goreleaser release --snapshot --clean
