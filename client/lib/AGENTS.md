This is a file designed for AI coding tools, to give the lay of the land of the project.

`client/lib/` is the browser-side frontend — the Gimbal shell environment. It contains ~30 subdirectories, each typically holding one "command" or "widget" implemented as an ES module.

## DO NOT

- **Do NOT start `gritsd`** — the production server is always running on the host. Starting a second instance will break things. Frontend changes are verified in a browser with the existing server.
- **Do NOT modify `config.json`** in the project root — the server depends on it.

## How it works

The entry point is `window.gimbal` — a `GimbalClient` (`gimbal/client.js`). It wraps a `GritsClient` for low-level filesystem access. All interactions are method calls on `GimbalClient` (for global commands) or `GimbalPath` (for filesystem ops).

Three types share a **dispatch proxy** mechanism (`gimbal/dispatch.js`):
- **GimbalClient** (`gimbal`) — the central client. Methods that aren't built-in (`volume`, `p`, `eval`, ...) are dispatched to `lib/<name>/main.js`. `gimbal.login()` or `gimbal.help()`.
- **GimbalPath** — a wrapper around a path string. Paths are absolute (`/home/foo`) or volume-prefixed (`//client/lib/bar`). Dispatchable so `path.read()`, `path.ls()` work. Not thenable.
- **GimbalResult** — a lazy async value. Created by the dispatch mechanism to wrap command invocations. Thenable — awaiting it runs the command pipeline.

Dispatch proxy contract: unknown method calls on any of these three types are routed to `lib/<name>/main.js`, which exports `invoke(gimbal, prev, ...args)`.

## Command module contract

### `invoke(gimbal, prev, ...args)`

| Parameter | Type | Meaning |
|-----------|------|---------|
| `gimbal` | `GimbalClient` | Always the GimbalClient (dispatch proxy) |
| `prev` | `GimbalClient` | Called as `gimbal.command(...)` — global commands |
| `prev` | `GimbalPath` | Called as `path.command(...)` — filesystem commands |
| `prev` | any other value | **Rejected** — throw immediately |
| `args` | string, GimbalPath, plain object | See per-command rules below |

### Universal argument rules

Every command follows the same strict positional pattern:

```
args[0]     — optional relative path (string) — resolved via prev.p(str)
args[last]  — optional options bag (plain object)
everything else — ERROR
```

**Rules:**
- `args[0]` must be a string (resolved relative to `prev` via `prev.p(str)`) or (for `diff`) a GimbalPath
- The options bag must be a plain object, and must be the last arg
- Any arg that doesn't match the expected type for its position → **ERROR**

### Per-command argument contract

| Command | prev | args[0] | args[1] | args[last] | Notes |
|---------|------|---------|---------|------------|-------|
| login, logout, whoami, home, help, test, download, upload, message, facl | GimbalClient | Per-command | — | Optional opts | Global commands |
| **mkdir**, **rm**, **rmdir** | GimbalPath | Optional string (rel path) | — | Optional opts | Operates on `prev/rel` |
| **read** | GimbalPath | Optional string (rel path) | — | — | Reads `prev/rel` |
| **ls** | GimbalPath | **ERROR** | — | — | Only lists `prev` |
| **write**, **append** | GimbalPath | Optional string (rel path) | Required content | Optional opts | Content = string/Response/Uint8Array/ArrayBuffer |
| **ln** | GimbalPath | Required string or GimbalPath (dest) | — | Optional opts | Links `prev` → `prev.p(dest)` or `dest` |
| **diff** | GimbalPath or GimbalClient | Required string or GimbalPath | — | Optional opts | If GimbalClient, two strings/GimbalPaths in args. Strings resolve via `prev.p()` (GimbalPath) or `gimbal.p()` (GimbalClient). |

**No cp** — use `ln` instead (copy-on-write link).

### Path resolution

When `args[0]` is a string, it's resolved **relative to `prev`**:
```js
// home.p('subdir') — creates new GimbalPath
// home.mkdir('subdir') — resolves 'subdir' relative to home
const target = prev.p(str);
```

For absolute paths, pass a GimbalPath instead: `home.mkdir(gimbal.p('/absolute/path'))`.

### Return values

- **Plain value** (string, array, object, null) — returned as-is, wrapped in dispatch proxy for chaining.
- **GimbalResult** — for async work. The dispatch mechanism flattens nested GimbalResults automatically.
- Commands should NOT return `Response` objects except `upload`, `download`, and `test` (which stream progressive output).

### Help string

Each module exports a `help` string shown by `gimbal.help('cmd')`.

## Core libraries

| Directory | Files | Purpose |
|-----------|-------|---------|
| `gimbal/` | `client.js`, `dispatch.js`, `result.js`, `path.js`, `volume.js`, `glob.js`, `overlay.js` | Core client — GimbalClient, GimbalResult, GimbalPath, Volume, dispatch proxy, glob |
| `grits/` | `GritsClient.js`, `MirrorManager.js`, `HashVerifier.js`, `PerformanceTracker.js` | Client-side filesystem |
| `style/` | `style.js`, `style.css` | Design tokens — colors, spacing, typography |
| `serviceworker/` | `grits-serviceworker.js`, `test.html` | Service worker for offline/PWA |

## Shell commands (each `dir/main.js`)

### Filesystem operations (take path from prev or args)

`ls`, `cp`, `mv`, `rm`, `mkdir`, `rmdir`, `ln`, `diff`, `read`, `write`, `append`, `unzip`, `path`

### Non-filesystem (require GimbalClient as prev)

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
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/src.txt').w('hello')`);
      await gimbal.eval(`gimbal.p('${scratch}/src.txt').cp(gimbal.p('${scratch}/dest.txt'))`);
      const text = await gimbal.eval(`gimbal.p('${scratch}/dest.txt').read()`);
      if (text !== 'hello') throw new Error('copy failed');
    },
  },
];
```

### How it works

1. **Discovery** — `test()` scans `lib/*/` for directories containing a `test.js` file
2. **Filtering** — run a subset with `test('cp', 'echo')`
3. **Isolation** — each test gets a unique scratch directory at `//primary/tmp/gimbal-test/<random>` (auto-created with `mkdir -p`)
4. **Test signature** — `fn(gimbal, scratch)` where `gimbal` gives access to `gimbal.eval()`, `gimbal.resolvePath()`, and `gimbal.grits.volume()`, and `scratch` is the scratch path string
5. **Assertions** — throw on failure: `throw new Error('expected ...')`
6. **Output** — streamed as plain text with ✓/✗ marks and a summary

### Common patterns

| What | How |
|---|---|---|
| Run a command | `await gimbal.eval(\`gimbal.p('${scratch}/path').cmd()\`)` |
| Read file content | `await gimbal.eval(\`gimbal.p('${scratch}/f').read()\`)` |
| Write file | `await gimbal.eval(\`gimbal.p('${scratch}/f').w('data')\`)` |
| Check file exists | `gimbal.grits.volume(gimbal._serverUrl, r.volumeName).lookup(r.path)` |
| Check file is gone | Catch `"not found"` from `lookup()` |
| Expect an error | Catch the error from `gimbal.eval()` and check `e.message` |

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
