.PHONY: test lint build release-gitlab

test:
	go test ./... -count=1

lint:
	go vet ./...
	@test -z "$$(gofmt -l . | tee /dev/stderr)"

build:
	go build ./...

# Build the release binaries and publish them as a GitLab release on the
# internal instance. Version comes from cmd/xpc/main.go; the matching git tag
# must already be pushed. Auth via GITLAB_TOKEN or the glab-cli token.
# See scripts/release-gitlab.sh for the full mechanism + env overrides.
release-gitlab:
	@bash scripts/release-gitlab.sh
