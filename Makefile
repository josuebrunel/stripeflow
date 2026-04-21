.PHONY: test coverage clean

# Run tests
test:
	go test -v ./...

# Run tests with coverage and generate an HTML report
coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	go tool cover -html=coverage.out -o coverage.html

# Clean up generated files
clean:
	rm -f coverage.out
