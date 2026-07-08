.PHONY: build test lint fmt tidy clean

build:
	go build -o dist/readignore ./cmd/readignore

test:
	go test ./... -race -cover

lint:
	golangci-lint run

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

clean:
	rm -rf dist/
