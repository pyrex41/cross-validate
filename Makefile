.PHONY: test lint build

test:
	go test ./... -count=1

lint:
	go vet ./...
	@test -z "$$(gofmt -l . | tee /dev/stderr)"

build:
	go build ./...
