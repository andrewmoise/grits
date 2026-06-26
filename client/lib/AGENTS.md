This is a file designed for AI coding tools, to give the lay of the land of the project.

`client/lib/` is the browser-side frontend — the Gimbal shell environment. It contains ~30 subdirectories, each typically holding one "command" or "widget" implemented as an ES module.

## How it works

The shell (`gimbal/gsh.js`) evaluates user input via `new Function('gsh', 'gwm', '_', '__', src)` — no `with`, no bare-identifier magic. `gsh` (the GimbalShell) and `gwm` (window manager) are the only globals in scope. All commands are called as methods on `gsh` or on a `GimbalPath` object.

Three core types share a **dispatch proxy** mechanism (`gimbal/dispatch.js`):
- **GimbalShell** (`gsh`) — the shell object. Methods that aren't built-in (`fork`, `eval`, `resolvePath`, ...) are dispatched to `lib/<name>/main.js`. `gsh.p('/path')` or `gsh.test()`.
- **GimbalPath** — a wrapper around an absolute path string. Dispatchable so `path.ls()`, `path.read()`, etc. work. Not thenable.
- **GimbalResult** — a lazy async value. Created by the dispatch mechanism to wrap command invocations. Thenable — awaiting it runs the command pipeline.

Dispatch proxy contract: unknown method calls on any of these three types are routed to `lib/<name>/main.js`, which exports `invoke(prev, ...args)`.

## Command module contract

### `invoke(prev, ...args)`

| Parameter | Type | Meaning |
|-----------|------|---------|
| `prev` | `GimbalShell` | Called as `gsh.command(...)` — use positional args |
| `prev` | `GimbalPath` | Called as `path.command(...)` — work on this path |
| `prev` | `GimbalResult` | Previous pipeline stage — await then retry |
| `prev` | other value | Direct value from pipeline |
| `args` | mixed | Positional arguments from user call |

### Return values

- **Plain value** (string, array, object, null) — returned as-is, wrapped in dispatch proxy for chaining.
- **GimbalResult** — for async work. The dispatch mechanism flattens nested GimbalResults automatically.
- Commands should NOT return `Response` objects except `upload`, `download`, and `test` (which stream progressive output).

### Path arguments

Commands should accept paths in any of these forms:
1. A `GimbalPath` object — canonical form
2. An absolute string starting with `/` — converted to `GimbalPath('/path', shell)`
3. A relative string — resolved via `shell.resolvePath()` and converted

Use the shared helper from `gimbal/path-util.js`:

```js
import { resolvePathArg } from '../gimbal/path-util.js';

export function invoke(prev, ...args) {
  const p = resolvePathArg(prev, args);
  if (!p) throw new Error('cmd: need a path');
  // p is a GimbalPath
}
```

For two-path commands (`cp`, `mv`, `ln`, `diff`), use `resolvePathArg` for the first path (from `prev`/`args[0]`) and find the second in the remaining args.

### Help string

Each module exports a `help` string shown by `gsh.help('cmd')`.

## Core libraries

| Directory | Files | Purpose |
|-----------|-------|---------|
| `gimbal/` | `gsh.js`, `dispatch.js`, `result.js`, `path.js`, `path-util.js`, `glob.js`, `overlay.js` | Shell environment — GimbalShell, GimbalResult, GimbalPath, dispatch proxy, path resolution, glob |
| `grits/` | `GritsClient.js`, `MirrorManager.js`, `HashVerifier.js`, `PerformanceTracker.js` | Client-side filesystem |
| `style/` | `style.js`, `style.css` | Design tokens — colors, spacing, typography |
| `serviceworker/` | `grits-serviceworker.js`, `test.html` | Service worker for offline/PWA |

## Shell commands (each `dir/main.js`)

### Filesystem operations (take path from prev or args)

`ls`, `cp`, `mv`, `rm`, `mkdir`, `rmdir`, `ln`, `diff`, `read`, `write`, `append`, `unzip`, `path`

### Non-filesystem (require GimbalShell as prev)

`login`, `logout`, `whoami`, `help`, `test`, `home`, `upload`, `download`, `message`

### UI / Widget launchers

`gterm`, `edit`, `codemirror`, `files`, `iframe`, `markdown`, `inbox`

## Frontend unit testing

Tests live in `lib/<name>/test.js` and are run via `test()` (the `lib/test/main.js` runner).

### Test structure

Each test file exports a `tests` array of `{ label, fn }` objects:

```js
export const tests = [
  {
    label: 'cp copies a file',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/src.txt').cp(gsh.path('${scratch}/dest.txt'))`);
      const text = await shell.eval(`gsh.path('${scratch}/dest.txt').read()`);
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
|---|---|---|
| Run a command | `await shell.eval(\`gsh.p('${scratch}/path').cmd()\`)` |
| Read file content | `await shell.eval(\`gsh.p('${scratch}/f').read()\`)` |
| Write file | `await shell.eval(\`gsh.p('${scratch}/f').w('data')\`)` |
| Check file exists | `shell._vol(r.serverUrl, r.volume).lookup(r.path)` |
| Check file is gone | Catch `"not found"` from `lookup()` |
| Expect an error | Catch the error from `shell.eval()` and check `e.message` |

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
| `message/`                  | Message compose widget       | `@cell compose-widget`     |
| `files/`                    | File browser                 | `@cell files-widget`      |
| `codemirror/`               | Code editor                  | `@cell codemirror-widget` |
| `project/`                  | Project/tracker panel        | `@cell tracker-widget`    |
| `iframe/`                   | iframe web viewer            | `@cell iframe-widget`     |
| `edit/`                     | Editor (thin wrapper around codemirror) | (none yet)      |
| `inbox/`                    | Inbox reader — lists messages from `local/inbox/` with expand/trash | `@cell inbox-widget` |
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
