BIN      := bin/filedrop
export CGO_ENABLED := 0

.PHONY: build run test vet lint e2e clean

build:
	go build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/server

run: build
	./$(BIN)

test:
	go test ./...

vet:
	go vet ./...

lint: vet
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

e2e: build
	bash scripts/e2e.sh

clean:
	rm -rf bin
