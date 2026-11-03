BINARY := bin/clashctl
VERSION ?= dev
RELEASE_DIR ?= dist
MODULE := github.com/kerolt/clashctl-go
LDFLAGS := -s -w -X $(MODULE)/internal/app.Version=$(VERSION)

.PHONY: build release test vet race verify clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/clashctl

release:
	rm -rf $(RELEASE_DIR)
	mkdir -p $(RELEASE_DIR)
	for target in linux/amd64 linux/arm64 linux/386 linux/arm; do \
		target_os=$${target%/*}; \
		target_arch=$${target#*/}; \
		GOOS=$$target_os GOARCH=$$target_arch CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(RELEASE_DIR)/clashctl-$$target_os-$$target_arch ./cmd/clashctl; \
	done
	(cd $(RELEASE_DIR) && sha256sum clashctl-*) > $(RELEASE_DIR)/SHA256SUMS

test:
	go test ./...

vet:
	go vet ./...

race:
	go test -race ./...

verify: test vet
	test -z "$$(gofmt -l cmd internal)"

clean:
	rm -f $(BINARY)
	rm -rf $(RELEASE_DIR)
