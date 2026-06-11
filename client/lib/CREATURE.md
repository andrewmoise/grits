This is a file designed for AI coding tools, to give the lay of the land of the project.

`client/lib/` is the browser-side frontend — the Gimbal shell environment. It contains ~36 subdirectories, each typically holding one "command" or "widget" implemented as an ES module. There's also a human-oriented `README.md` here with usage info.

## How it works

The shell (`gimbal/gsh.js`) evaluates user input via `new Function()` with a `with` proxy so that unknown identifiers auto-dispatch to `lib/<name>/main.js`. Each command module exports `async function invoke(shell, previous, args)` plus an optional `help` string. The last argument, if a plain object, is treated as an options map.

Commands return a `Result` (thenable), which wraps a proxy so chained method calls auto-dispatch to the next command in the pipeline (e.g. `upload().to(filename)`).

## Core libraries

| Directory        | Files                                                                 | Purpose |
|------------------|-----------------------------------------------------------------------|---------|
| `gimbal/`        | `gsh.js`, `glob.js`, `overlay.js`, `index.html`, `login.html`         | Shell environment — GimbalShell, Result, path resolution, eval, glob expansion (~455 lines in gsh.js) |
| `grits/`         | `GritsClient.js`, `MirrorManager.js`, `HashVerifier.js`, `PerformanceTracker.js` | Client-side filesystem — `GritsClient` (cache), `GritsVolume` (server ops), `GritsFile` (file handles), mirror management, content integrity verification (~1564 lines in GritsClient.js) |
| `style/`         | `style.js`, `style.css`                                                | Single source of truth for design tokens — colors, spacing, typography; all derived color utilities live here (~389 lines) |
| `vendor/`        | `json-stringify-pretty-compact/`                                        | Vendored npm package for compact JSON pretty-printing |
| `serviceworker/` | `grits-serviceworker.js`, `test.html`                                   | Service worker for offline/PWA — caches Grits content client-side, handles SW update flow |

## Shell commands (each `dir/main.js`)

Standard Unix-like commands: `cat`, `cd`, `cp`, `echo`, `grep`, `ln`, `ls`, `mkdir`, `mv`, `pwd`, `rm`, `rmdir`, `wget`

Gimbal-specific: `from(filename)` / `to(filename)` for `<` / `>` style piping, `download(url)`, `upload()`, `help()`, `test()`, `unzip()`

Each command directory typically has `main.js` and optionally `test.js`.

## Widgets (GWM window manager components)

Widgets are windows within the Gimbal window environment. Each has a `main.js` (the shell command to launch it) and a `gwm-widget.js` (the actual widget implementation):

| Directory                   | Widget                       | `@cell` tag               |
|-----------------------------|------------------------------|---------------------------|
| `gterm/`                    | Terminal widget              | `@cell terminal-widget`   |
| `files/`                    | File browser                 | `@cell files-widget`      |
| `codemirror/`               | Code editor                  | `@cell codemirror-widget` |
| `project/`                  | Project/tracker panel        | `@cell tracker-widget`    |
| `iframe/`                   | iframe web viewer            | `@cell iframe-widget`     |
| `edit/`                     | Editor (thin wrapper around codemirror) | (none yet)      |
| `jqterminal/terminal.js`    | Terminal widget (alternate)  | `@cell terminal-widget`   |

The window manager itself (`gwm`) doesn't have its own directory yet — its logic is spread across `gimbal/` files. Widgets implement a decoration interface (icon, rightButtons, onCloseRequest) and receive a `controls` interface (setTitle) from the shell.

## Other directories

| Directory   | Contents |
|-------------|----------|
| `lib/lib/`  | `main.js` — the lib command itself |
| `plumber/`  | Empty (planned but not implemented) |
| `shop/`     | `SHOP_DATA_CONTRACT.md` — data contract docs |
| `test/`     | `main.js` — test runner |

If you observe the project to be out of sync with any CREATURE.md files, or if there is a module that doesn't have enough explanation to quickly get the lay of the land, feel free to update (sparingly!) to add more explanations or update them. As a general guideline, any source directory (or batch of source directories) that has more than 10 source files probably needs its own CREATURE.md file.

Cheers
