.PHONY: test test-race fmt vet check contracts-check contracts-accept dev-seed dev-api dev-publish

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

# Local development only: data/technews.db under the worktree root, never production.
DEV_JWT_SECRET ?= local-dev-secret

dev-seed:
	go run ./cmd/devseed

dev-api:
	JWT_SECRET=$${JWT_SECRET:-$(DEV_JWT_SECRET)} COLLECTOR_ENABLED=$${COLLECTOR_ENABLED:-1} go run ./cmd/api

# Publishes queued candidates for real (Gemini + S3). Requires GEMINI_API_KEY and
# AWS credentials; uses a dev S3 prefix so test images never mix with production.
dev-publish:
	JWT_SECRET=$${JWT_SECRET:-$(DEV_JWT_SECRET)} COLLECTOR_ENABLED=$${COLLECTOR_ENABLED:-1} \
	PUBLISHER_ENABLED=1 S3_FEATURE_IMAGE_PREFIX=$${S3_FEATURE_IMAGE_PREFIX:-dev-features} go run ./cmd/api
