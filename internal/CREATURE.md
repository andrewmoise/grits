<!-- CREATURE.md — AI context file for this project cell -->

This is a file designed for AI coding tools, to give the lay of the land of the project.

`internal/` houses all Go source for the Grits backend, split into two packages:

* `internal/grits/` — Core library: blob storage, name store (Merkle-tree namespace), configuration, shared types and interfaces
* `internal/gritsd/` — Server daemon: module system, all server modules, server lifecycle, periodic tasks, long-running jobs, import utilities

The `grits` package is the foundation — it defines the `BlobStore`, `NameStore`, and `FileNode` interfaces that everything else builds on. The `gritsd` package implements the actual server that wires these together via a plugin-style module architecture.

For more information, you should look at:

* `internal/grits/CREATURE.md` — Core library: blob store, name store, config, structures
* `internal/gritsd/CREATURE.md` — Server daemon: module system, all modules, server lifecycle

If you observe the project to be out of sync with any CREATURE.md files, or if there is a module that doesn't have enough explanation to quickly get the lay of the land, feel free to update (sparingly!) to add more explanations or update them. As a general guideline, any source directory (or batch of source directories) that has more than 10 source files probably needs its own CREATURE.md file.

Cheers
