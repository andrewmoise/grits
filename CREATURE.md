<!-- CREATURE.md — AI context file for this project cell -->

This is a file designed for AI coding tools, to give the lay of the land of the project.

Gimbal/Grits is a web framework that makes web sites more direct and flexible to interact with. The backend (Grits) is a content-addressable Merkle-tree filesystem with a pluggable module system. The frontend (Gimbal) is a browser-based shell and window manager that talks to Grits over HTTP.

The project is split into Go backend code, a JavaScript browser client, and a few CLI entry points. Each major subsystem has its own CREATURE.md explaining its internal structure.

For more information, you should look at:

* `internal/CREATURE.md` — The Go backend: core library (`internal/grits/`) and server daemon (`internal/gritsd/`)
* `client/lib/CREATURE.md` — The browser client: shell, commands, widgets, and core libraries
* `cmd/CREATURE.md` — CLI entry points: gritsd, grits, certbot-helper, testbed

If you observe the project to be out of sync with any CREATURE.md files, or if there is a module that doesn't have enough explanation to quickly get the lay of the land, feel free to update (sparingly!) to add more explanations or update them. As a general guideline, any source directory (or batch of source directories) that has more than 10 source files probably needs its own CREATURE.md file.

Cheers
