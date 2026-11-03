BINARY := bin/clashctl

.PHONY: build test vet race verify clean

build:
	go build -trimpath -o $(BINARY) ./cmd/clashctl

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
