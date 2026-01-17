.PHONY: lint test test-race build clean

lint:
	golangci-lint run ./...

test:
	go test -v ./...

test-race:
	go test -race -v ./...

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="-w -s" -o bin/server ./cmd/server

clean:
	rm -rf bin/
	go clean
