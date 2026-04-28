BIN := bin/codedojo

.PHONY: build test lint smoke e2e-reviewer e2e clean

build:
	go build -ldflags "-X github.com/dhruvmishra/codedojo/internal/cli.version=dev -X github.com/dhruvmishra/codedojo/internal/cli.commit=$$(git rev-parse --short HEAD 2>/dev/null || echo none)" -o $(BIN) ./cmd/codedojo

test:
	go test ./...

lint:
	golangci-lint run ./...

smoke:
	go run ./cmd/codedojo smoke

e2e-reviewer:
	go test ./internal/cli -run TestRunReviewScriptedSubmission -count=1

e2e: smoke e2e-reviewer

clean:
	rm -rf bin dist coverage.out
