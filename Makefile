.DEFAULT_GOAL := build

.PHONY: build
build:
	go build -o tripwire

.PHONY: clean
clean:
	rm tripwire

.PHONY: test
test: build
	go run gotest.tools/gotestsum@latest `go list ./pkg`
