This is a file designed for AI coding tools, to give the lay of the land of the project.

`internal/gritsd/` implements the Grits server daemon. It uses a plugin-style module architecture where almost all functionality is provided by modules loaded from `config.json`.

## Core files

- **server.go** — `Server` struct: holds Config, BlobStore, Volumes map, Modules list, periodic task runner, job tracker, shutdown orchestration (~203 lines)
- **modules.go** — Module interface, module factory (creates modules from config by type string), dependency resolution and topological sort, `LoadModules` entry point (~394 lines)

## Module files (each `module_*.go` implements one module type)

| File                       | Module type      | Purpose |
|----------------------------|------------------|---------|
| `module_volume.go`         | `volume`         | Local writable volume — implements `Volume` interface wrapping `grits.NameStore` with blob import helpers (~383 lines) |
| `module_http.go`           | `http`           | HTTP API server — handles `lookup`, `link`, `get`, `put`, `metadata` and all `grits/v1/...` endpoints; TLS, cert loading, rate limiting, CORS (~1361 lines) |
| `module_auth.go`           | `auth`           | Authentication — argon2 password hashing, read/write path whitelists, principal tracking (~299 lines) |
| `module_mount.go`          | `mount`          | FUSE mount — exposes a Grits volume as a real Linux filesystem via go-fuse (~1613 lines) |
| `module_mirror.go`         | `mirror`         | Content mirror — can fetch and cache content from an origin server; acts as a read-through cache (~302 lines) |
| `module_origin.go`         | `origin`         | Content origin — accepts mirror registrations, tracks mirror health, replicates content to mirrors (~343 lines) |
| `module_remote.go`         | `remote`         | Remote volume — access a volume on another Grits server over HTTP, with local blob caching (~523 lines) |
| `module_cmdline.go`        | `cmdline`        | Unix socket command pipe — lets `bin/grits` send admin commands (ping, import, adduser, deluser) (~308 lines) |
| `module_serviceworker.go`  | `serviceworker`  | Service worker support — injects SW bootstrap into HTTP responses, handles SW update flow (~332 lines) |
| `module_startup.go`        | `startup`        | Startup commands — runs a list of backend commands on server start (~44 lines) |
| `module_peer.go`           | `peer`           | Peer networking — registers with a tracker, maintains heartbeat for NAT traversal / mesh networking (~210 lines) |
| `module_tracker.go`        | `tracker`        | Tracker — accepts peer registrations, assigns DNS subdomains, heartbeats, peer discovery (~522 lines) |
| `module_pin.go`            | `pin`            | Pin module — holds FileNode references to prevent garbage collection of specific paths (~69 lines) |

## Supporting files

| File              | Purpose |
|-------------------|---------|
| `validation.go`   | Regex-based input validation for blob addrs, paths, hostnames, usernames, etc. (~106 lines) |
| `refholder.go`    | ReferenceHolder — holds Releasable objects and auto-releases them after a timeout (~124 lines) |
| `utils.go`        | ImportLocalDir and helpers — walks a real filesystem tree and imports it into a volume (~291 lines) |
| `tasks.go`        | Periodic task framework — AddPeriodicTask / StopPeriodicTasks (~31 lines) |
| `job.go`          | JobDescriptor — tracks long-running operations with stage/completion/cancel (~77 lines) |
| `certbot.go`      | Certbot integration — launches certbot-helper subprocess, manages cert PEMs |

## Module architecture

Modules implement the `Module` interface (`Start()`, `Stop()`, `GetModuleName()`, `GetConfig()`, `GetDependencies()`). The `Volume` sub-interface adds blob and namespace operations. Dependencies can be `DependOptional`, `DependRequired`, or `DependAutoCreate` (which spins up a default instance if missing). Modules are topologically sorted on startup so dependencies start before their dependents.

If you observe the project to be out of sync with any AGENTS.md files, or if there is a module that doesn't have enough explanation to quickly get the lay of the land, feel free to update (sparingly!) to add more explanations or update them. As a general guideline, any source directory (or batch of source directories) that has more than 10 source files probably needs its own AGENTS.md file.

Cheers
