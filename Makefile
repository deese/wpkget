BINARY     := wpkget
CMD        := ./src/cmd/wpkget
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS    := -s -w -X main.version=$(VERSION)

ifeq ($(OS),Windows_NT)
  OUT := $(BINARY).exe
else
  OUT := $(BINARY)
endif

.PHONY: build install clean

build:
	go build -ldflags="$(LDFLAGS)" -o $(OUT) $(CMD)

install: build
	go build -ldflags="$(LDFLAGS)" -o $(shell go env GOPATH)/bin/$(OUT) $(CMD)

clean:
	rm -f $(BINARY) $(BINARY).exe
