BINARY = kubernetes-eager-oom-killer

.PHONY: build format test clean

build:
	go build -o $(BINARY) .

format:
	gofmt -w .

test:
	go test -v ./...

clean:
	rm -f $(BINARY)
