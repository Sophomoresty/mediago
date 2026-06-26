BINARY = mediago
MODULE = github.com/nichuanfang/medigo
VERSION = 0.1.0

.PHONY: build build-all clean test

build:
	go build -ldflags "-s -w" -o $(BINARY) ./cmd/mediago

build-all:
	GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o dist/$(BINARY).exe ./cmd/mediago
	GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o dist/$(BINARY)-linux ./cmd/mediago
	GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o dist/$(BINARY)-mac ./cmd/mediago

test:
	go test ./...

clean:
	rm -f $(BINARY)
	rm -rf dist/
