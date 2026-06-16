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

## Frontend unit testing

Tests live in `lib/<name>/test.js` and are run via `test()` (the `lib/test/main.js` runner).

### Test structure

Each test file exports a `tests` array of `{ label, fn }` objects:

```js
export const tests = [
  {
    label: 'cp copies a file',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      await shell.eval(`cp('${scratch}/src.txt', '${scratch}/dest.txt')`);
      const text = await shell.eval(`cat('${scratch}/dest.txt').toText()`);
      if (text !== 'hello') throw new Error('copy failed');
    },
  },
];
```

### How it works

1. **Discovery** — `test()` scans `lib/*/` for directories containing a `test.js` file
2. **Filtering** — run a subset with `test('cp', 'echo')`
3. **Isolation** — each test gets a unique scratch directory at `//primary/tmp/gimbal-test/<random>` (auto-created with `mkdir -p`)
4. **Test signature** — `fn(shell, scratch)` where `shell` gives access to `shell.eval()`, `shell.resolvePath()`, and `shell._vol()`, and `scratch` is the scratch path string
5. **Assertions** — throw on failure: `throw new Error('expected ...')`
6. **Output** — streamed as plain text with ✓/✗ marks and a summary

### Common patterns

| What | How |
|---|---|
| Run a command | `await shell.eval(\`cmd('${scratch}/path')\`)` |
| Read file content | `await shell.eval(\`cat('${scratch}/f').toText()\`)` |
| Check file exists | `shell._vol(r.serverUrl, r.volume).lookup(r.path)` |
| Check file is gone | Catch `"not found"` from `lookup()` |
| Expect an error | Catch the error from `shell.eval()` and check `e.message` |
| Write for a test | `await shell.eval(\`echo('data').to('${scratch}/f')\`)` |

### Options

| Flag | Effect |
|---|---|
| `{v:1}` | Show full stack traces on failure |
| `{ff:1}` | Fail fast — stop at first failure |

Run from the Gimbal shell: `test()` or `test('cp', 'echo')`.

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

If you observe the project to be out of sync with any AGENTS.md files, or if there is a module that doesn't have enough explanation to quickly get the lay of the land, feel free to update (sparingly!) to add more explanations or update them. As a general guideline, any source directory (or batch of source directories) that has more than 10 source files probably needs its own AGENTS.md file.

Cheers
