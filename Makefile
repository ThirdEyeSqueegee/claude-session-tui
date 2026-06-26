BINARY  := cst
PREFIX  := $(HOME)/.local/bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || date +%Y%m%d)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build install test race fmt vet clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install: build
	mkdir -p $(PREFIX)
	install -m 0755 $(BINARY) $(PREFIX)/$(BINARY)
	@echo "installed $(PREFIX)/$(BINARY) ($(VERSION))"
	@echo "run 'cst' — copy config.example.toml to ~/.config/cst/config.toml to customize"

test:
	go test ./...

race:
	go test -race ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -f $(BINARY)
