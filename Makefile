BIN := bin/codedojo

.PHONY: build test lint smoke e2e-reviewer e2e-newcomer e2e demo-review images clean

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

e2e-newcomer:
	go test ./internal/cli -run TestRunLearnScriptedReimplementation -count=1

e2e: smoke e2e-reviewer e2e-newcomer

demo-review:
	scripts/demo-review.sh

images:
	docker build -t codedojo/go:1.23 configs/images/go
	docker build -t codedojo/python:3.12 configs/images/python

clean:
	rm -rf bin dist coverage.out
