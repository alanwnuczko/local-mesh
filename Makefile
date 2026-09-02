# M-6: conditionally set binary extension — .exe only on Windows.
ifeq ($(OS),Windows_NT)
    EXT := .exe
else
    EXT :=
endif

# L-4: declare all non-file targets as phony so they always run.
.PHONY: build test lint cross-compile clean run test-race \
	cross-windows cross-windows-arm64 \
	cross-linux cross-linux-arm64 \
	cross-darwin cross-darwin-amd64

BINARY   := local-mesh
CMD      := ./cmd/local-mesh
GOFLAGS  :=

build:
	go build $(GOFLAGS) -o $(BINARY)$(EXT) $(CMD)

run: build
	./$(BINARY)$(EXT)

test:
	go test ./...

test-race:
	CGO_ENABLED=1 go test -race ./...

lint:
	go vet ./...

# Cross-compile targets matching GoReleaser (linux/windows/darwin × amd64/arm64)
cross-compile: cross-windows cross-windows-arm64 cross-linux cross-linux-arm64 cross-darwin cross-darwin-amd64

cross-windows:
	GOOS=windows GOARCH=amd64 go build -o dist/$(BINARY)-windows-amd64.exe $(CMD)

cross-windows-arm64:
	GOOS=windows GOARCH=arm64 go build -o dist/$(BINARY)-windows-arm64.exe $(CMD)

cross-linux:
	GOOS=linux GOARCH=amd64 go build -o dist/$(BINARY)-linux-amd64 $(CMD)

cross-linux-arm64:
	GOOS=linux GOARCH=arm64 go build -o dist/$(BINARY)-linux-arm64 $(CMD)

cross-darwin:
	GOOS=darwin GOARCH=arm64 go build -o dist/$(BINARY)-darwin-arm64 $(CMD)

cross-darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build -o dist/$(BINARY)-darwin-amd64 $(CMD)

clean:
	rm -f $(BINARY)$(EXT) dist/*
