.PHONY: all build run test clean deps

# The name of our output binary
APP_NAME := aster-bot

# Variables
CMD_DIR := ./cmd/bot
CONFIG_FILE := config.yaml

all: build

# Download local dependencies
deps:
	go mod download
	go mod tidy

# Build the bot binary
build: deps
	go build -o bin/$(APP_NAME) $(CMD_DIR)

# Run the bot directly
run:
	go run $(CMD_DIR)/main.go $(CONFIG_FILE)

# Run tests across all packages
test:
	go test -v ./...

# Clean build artifacts
clean:
	rm -rf bin/
	go clean
