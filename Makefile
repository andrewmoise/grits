.PHONY: all build audit deps fix test

all: build audit

build:
	go build -o bin/certbot-helper cmd/certbot-helper/main.go
	go build -o bin/grits          cmd/grits/main.go
	go build -o bin/gritsd         cmd/gritsd/main.go

test:
	go test ./internal/... ./cmd/...

deps:
	cd client/lib && npm install --ignore-scripts --no-audit

audit:
	cd client/lib && npm audit

fix:
	cd client/lib && npm audit fix