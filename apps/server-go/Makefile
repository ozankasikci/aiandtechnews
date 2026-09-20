.PHONY: test test-race fmt vet check contracts-check contracts-accept

test:
	go test ./...

test-race:
	go test -race ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

check: test vet

contracts-check:
	./scripts/sync-contracts.sh --check
	go test ./internal/contracttest
	go test ./internal/app -run Contract

contracts-accept:
	@printf '%s\n' 'WARNING: review apps/server/contracts/node/contracts.json before accepting it into the generated Go mirror.'
	./scripts/sync-contracts.sh --accept
