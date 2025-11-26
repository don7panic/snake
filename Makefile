.PHONY: test lint build clean fmt vet

# Default target
all: fmt vet test build

# Build the project
build:
	go build -v ./...

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Format code
fmt:
	go fmt ./...

# Run go vet
vet:
	go vet ./...

# Run staticcheck (if available)
lint:
	@which staticcheck > /dev/null || (echo "Installing staticcheck..." && go install honnef.co/go/tools/cmd/staticcheck@latest)
	staticcheck ./...

# Clean build artifacts
clean:
	rm -f coverage.out coverage.html
	go clean -testcache

# Run all checks
check: fmt vet lint test

# Install dependencies
deps:
	go mod download
	go mod tidy