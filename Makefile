.PHONY: clean test

ecrm: go.* *.go cmd/ecrm/main.go
	go build -o $@ cmd/ecrm/main.go

clean:
	rm -rf ecrm dist/

test:
	go test -v ./...

install:
	go install github.com/fujiwara/ecrm/cmd/ecrm

dist:
	goreleaser build --snapshot --rm-dist
