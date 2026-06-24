.PHONY: all build audit deps fix test src

all: build src audit

build:
	go build -o bin/certbot-helper cmd/certbot-helper/main.go
	go build -o bin/grits          cmd/grits/main.go
	go build -o bin/gritsd         cmd/gritsd/main.go

test:
	go test ./internal/... ./cmd/...

deps:
	cd client/lib && npm install --ignore-scripts --no-audit
	go install golang.org/x/vuln/cmd/govulncheck@latest

audit:
	~/go/bin/govulncheck ./internal/... ./cmd/...
	cd client/lib && npm audit

fix:
	cd client/lib && npm audit fix

src:
	rm -rf content/src
	mkdir -p content/src
	cp -r .git content/src/.git
	cd content/src/.git && git update-server-info
	cp README.md TODO.md go.mod go.sum sample-config.json config.json content/src/
	cp AGENTS.md LICENSE Makefile REFERENCE.md content/src/
	cp -r internal doc client cmd content/src/
	mkdir -p content/src/content
	for d in content/*/; do \
		base=$$(basename "$$d"); \
		[ "$$base" = "src" ] && continue; \
		cp -r "$$d" "content/src/content/$$base"; \
	done