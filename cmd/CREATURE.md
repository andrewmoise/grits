This is a file designed for AI coding tools, to give the lay of the land of the project.

`cmd/` contains the Go entry points (`package main`). Each subdirectory builds a standalone binary.

| Directory                | Binary               | Purpose |
|--------------------------|----------------------|---------|
| `gritsd/main.go`         | `bin/gritsd`         | The server daemon — loads config, initializes `grits.Config` and `gritsd.Server`, starts modules, handles privilege drop from root, manages shutdown (~148 lines) |
| `grits/main.go`          | `bin/grits`          | CLI admin tool — connects to the running daemon via a Unix socket (`var/grits-cmd.pipe`), sends commands like `ping`, `import`, `adduser`, `deluser` (~95 lines) |
| `certbot-helper/main.go` | `bin/certbot-helper` | Certificate helper — runs as root (launched by gritsd before privilege drop), reads hostnames from stdin, runs certbot, writes cert/key PEM as JSON lines to stdout (~116 lines) |
| `testbed/main.go`        | (build tag `ignore`) | Test harness for mirror/origin/peer scenarios — spins up multiple server instances with various modules for integration testing (~670 lines, currently disabled with `//go:build ignore`) |

If you observe the project to be out of sync with any CREATURE.md files, or if there is a module that doesn't have enough explanation to quickly get the lay of the land, feel free to update (sparingly!) to add more explanations or update them. As a general guideline, any source directory (or batch of source directories) that has more than 10 source files probably needs its own CREATURE.md file.

Cheers
