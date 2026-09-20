.PHONY: test test-race fmt vet check

test:
	go test ./...

test-race:
	go test -race ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

check: test vet
