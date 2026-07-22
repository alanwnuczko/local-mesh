.PHONY: build test lint cross-compile clean run

BINARY   := local-mesh
CMD      := ./cmd/local-mesh
GOFLAGS  :=

build:
	go build $(GOFLAGS) -o $(BINARY).exe $(CMD)

run: build
	./$(BINARY).exe

test:
	go test ./...

test-race:
	CGO_ENABLED=1 go test -race ./...

lint:
	go vet ./...

# Cross-compile targets for distribution
cross-compile: cross-windows cross-linux cross-darwin

cross-windows:
	GOOS=windows GOARCH=amd64 go build -o dist/$(BINARY)-windows-amd64.exe $(CMD)

cross-linux:
	GOOS=linux GOARCH=amd64 go build -o dist/$(BINARY)-linux-amd64 $(CMD)

cross-darwin:
	GOOS=darwin GOARCH=arm64 go build -o dist/$(BINARY)-darwin-arm64 $(CMD)

clean:
	rm -f $(BINARY).exe dist/*
