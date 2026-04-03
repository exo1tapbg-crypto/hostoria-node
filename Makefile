BINARY   = hostoria-node
BUILD_DIR = build
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

LDFLAGS = -s -w \
  -X github.com/hostoria/hostoria-node/cmd.Version=$(VERSION) \
  -X github.com/hostoria/hostoria-node/internal/api/handlers.Version=$(VERSION)

.PHONY: all build build-linux-amd64 build-linux-arm64 clean test tidy

all: build

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) .

build-linux-amd64:
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY)-linux-amd64 .

build-linux-arm64:
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY)-linux-arm64 .

release: build-linux-amd64 build-linux-arm64
	@echo "Built release binaries:"
	@ls -lh $(BUILD_DIR)/$(BINARY)-linux-*

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -rf $(BUILD_DIR)
