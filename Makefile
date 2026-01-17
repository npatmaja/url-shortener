.PHONY: lint test build clean

lint:
	golangci-lint run ./...

test:
	go test -v ./...

build:
	go build -o bin/server ./cmd/server

clean:
	rm -rf bin/
