.PHONY: build test vet race check

build:
	go build ./...

test:
	go test -timeout 120s ./...

vet:
	go vet ./...

# Race detector requires CGO_ENABLED=1.
# Run this in CI to catch data races not covered by code review.
race:
	CGO_ENABLED=1 go test -race -timeout 180s ./...

check: build vet test
