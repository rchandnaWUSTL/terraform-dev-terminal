build:
	go build -o terraform-dev ./cmd/terraform-dev/

run: build
	PATH="$(HOME)/bin:$(PATH)" ./terraform-dev

install: build
	cp terraform-dev $(HOME)/bin/terraform-dev

test:
	go test ./...

vet:
	go vet ./...

.PHONY: build run install test vet
