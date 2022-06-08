all: test

fmt:
	go fmt ./...

test:
	go test ./... -coverprofile cover.out
