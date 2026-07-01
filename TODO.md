# In Progress

* Let editor open files
* Little RSS feed demo app
* Make test() streaming because of returning a Response
* markdown() -> md()
* Check the updates to login cookies; seem like they're timing out even when in active use
* Pin window to master slot in gwm
* Make more complete auth-timeout-force-re-login system (an endpoint to verify user-initiated action, and a way to configure auth timeouts per user)
* Let people reset their own passwords
* Fix tooltips on main icons
* Fix tooltips / full path things on titlebars


# Near Future

* Change rateLimit to just "limits"
* Extend click() to be able to handle right click
* Change access.json to JSONL
* Rename client/ to gimbal/
* Fix markdown titlebar
* gterm command Ctrl-Z for undo text editing
* More tools: Diff, patch
* Filesystem undo
* Better git self-hosting
* Mobile interface
* Fix the scrolling weirdness (scroll partway down a markdown document in the master, then move an element of the stack to a new position, and the MD document jumps back to the top)
* Remove indirection - just go import from locations without a `cp -r` in the Makefile
* Guest user auto-homedir-deletion
* Fix certbot browser badness when first accessing a new vhost, and browser visits http://{whatever}
* Make redirect from http:// to https://
* Make "const" work in the shell eval
* Make "//" work in the shell eval
* Import git tools libs
* Check remote volume functioning
* Check FUSE functioning (esp when writes happen outside FUSE)
* Some version of diff() + incorporating new changes
* ls() should return relative paths by default
* Clipboard copy for code sections in markdown
* Make gimbal.help() print a full list of all the commands
* Make purple titlebar icons in the examples

# Backlog



## shell tools

allow()/deny()/access() supporting directory recursion up and down

signup()

Defaults / config system

adduser() and deluser() from frontend

~~Make skel() function~~

Make editor "Save as" and editing of scratch files more sensible

Make rm() and rmdir() (at least) a lot more simple into a single multilink() (maybe after a quick check first)

bg() and Ctrl-Z

Make test() in the foreground once bg() exists

Some sort of toast for running commands in the background like bg()

.null()

time()

js() and json() (replacing all the .toWhatever() methods that aren't quite exactly right)

Make more secure login self test than test/test user

Make test() print failures as red, or a red X or something is probably easier

whoami() should return some better indication, if someone's login has expired

Lots of tools need to realize when their arguments aren't strings and react accordingly (login() in particular doesn't do good if it gets two options bags)

Make an indication when a response stream doesn't end with '\n'

.write() should overwrite unless {i:1} is specified, I guess

Make shift-up select the current line


## shell

__[n] reuse with Results — using the same history entry twice consumes the Response stream. Fix: buffer to bytes on storage, __(n) wraps in fresh Response on access.

isVoid FIXME — currently checks null and undefined in addition to VOID, which was supposed to be temporary.

cwd FIXME — empty string from backend needs to be treated as root, currently patched in cwdLabel.

Ctrl-Enter statement mode — full JS with let/const/function declarations persisting via a scope object, shared with normal Enter mode.

test() currently changes directory as it's running

Pass actual column width to _display() for correct pretty-print wrapping

Line the result __[n] up with the output, not the input

Sync up the /lib directory with a particular snapshot

Handle binary or very-large files a little better in terminal output

Detect un-handled options and give an error on those

Fix problem where glob() will return .. which will then get re-resolved

Tab completion

Don't add void returns to history

Do away with doHistory

Shell environment variables

Fix copy and paste from Gimbal shell

Make a better self-hosting setup

Fix test /tmp/tmp scratch dir

Move to $HOME/tmp (or better a GritsClient-created temp thing)

Change "command not found" error to a little more explanatory if someone shuts the server down during shell session

"access denied: undefined" message should be a little better

Maybe make a bash style of shell



## gwm

Syncing cwd between files() and cd() in a shell optionally

Hotkeys

Click in input field allows text selection without stealing focus

Hook files widget to start at a non-root permitted path

Make multi-file editing based on clicking titlebar to see a listing
(and same for files widget)

Make files widget sync to changes in the directory

Image / video / random binary file output in terminal

Make light mode

Add a menu (to edit() if nothing else)

ssh widget

Some indication when things disconnect

chat

Make it more obvious which widget has focus

Make "up" not discard the line we're currently working on, in gterm

Make the index.html title correspond to the vhost

Mouse focus

Make minimized windows deduplicate
Make minimized windows distinguishable based on their titles

Fix the markdown icon in the tray

Folder color to Orange

Better monospace font

Some sort of customizable command palette similar to acme

MOTD

Make "safe mode"

Rename gwm-widget.js to something more accurate now

Nice icons (clarity)


## grits client

Blob volume

Tmp volume

Look into possible prefetch loop issue

Add index.html to path lookups pre-emptively

XX Change GritsClient to be per serverUrl
(Probably superceded now; instead we need to look at having the SW bounce every blob read to immutable accesses to /grits/v1/blob in the core vhost)

s/cell/creature/g

Sort out a way to upload unlimited amounts of stuff without timing out the "PUTs get ref-held for 45 seconds only" issue

Lookup and link multiple things in one request when possible

Look into service worker not unregistering

Silent catches:
-- Truly problematic (2 sites):
1. project/gwm-widget.js:374-387 — catch (_) silently treats all errors (JSON parse failure, permission error, network timeout) as "project file doesn't exist" and overwrites with a fresh empty project. This can silently destroy the user's project data.
2. codemirror/gwm-widget.js:204-214 — Save failure logs to console but gives the user no visible feedback. The dirty flag stays true (so the close-warning barrier works), but the user can't tell whether Ctrl+S actually persisted.
-- Questionable — grace-degradation that silently masks non-obvious errors (7 sites):
3. grits/GritsClient.js:680 — catch (_) {} in _desyncMultiLink(). Treats a network error or permission denial identically to "file not found". The currentFile stays null, which is the right fallback, but a real problem would be invisible.
4. grits/GritsClient.js:1019-1022 — catch(() => new Promise(() => {})) in fast-walk path. If fastWalk has a bug, the error is silently discarded with a never-settling promise. At minimum should DEBUG && console.debug(...).
5. grits/GritsClient.js:1626 — catch (_) {} in cache unmarshal. Corrupt cache data silently ignored. Minor, but a console.warn would help.
6. grits/GritsClient.js:1662 — catch (_) {} in _collectMirrorStats(). Stats failures silently swallowed across all volumes.
7. grits/PerformanceTracker.js:129 — catch (_) { /* mirror stats unavailable */ }. Same pattern.
8. gimbal/glob.js:26,41,98 — Three catch(e){return;} / catch(e){return[];} sites. Glob silently skipping inaccessible directories is arguably correct shell-like behavior, but no log means bugs are invisible.
9. gimbal/gsh.js:350 — catch(e){throw new Error("command not found")}. If a command module has a syntax error or import failure, the user gets a misleading "command not found" instead of the actual error.



## Grits backend

Be smart about sending small blobs directly with the answer, for stuff like lookup(), to save RTT

Transition a bunch of stuff to JSONL

Great renaming of concepts

Finish service worker

Prune for path access on multilink

Make multilink (esp with assertions) atomic, with OCC probably

Change LookupResponse to just include metadata inline

Make mkdir('one/two') return a sensible error, not a 500

Change 404 returns during normal operations, so we don't spam the console

Verify that stores with missing blobs either still work, or else panic. Add tests for that case.

gzip, maybe

Implement quotas

Auth improvements
  * Make timeout configurable
  * Make non user initiated actions not remove the timeout
  * Handle multiple identities
  * More friendly error messages on expired identity / need to log in to access this resource
  * Better prompt for password for login
  * Add documentation
  * Make "role" things in addition to "who am I" type of things
  * Wildcard glob matching in auth module, not just "*" only

Some kind of better window, in the frontend, into HTTP traffic and requests (log / dashboard) if nowhere else

Clean up places where "primary" as the volume name is hard coded: `rootVolume` → `mainVolume` (module_auth.go), `rootVolume()` → `mainVolume()` (module_serviceworker.go), `rootVolume` variable name (cmd/testbed/main.go)

Check a little more thoroughly that multiple auth tokens at once work well

Eliminate /grits/v1/content

Make paths in APIs require an initial slash maybe

Make systemctl setup

Make a monitor for /sites/ that gets certs right away when a directory is created

Allow empty passwords (meaning login is disabled)

Let deluser remove home directory, too, with an argument

Docker, I guess

Put passwords in the user's home directory, and back out the password strength checking

Allow users to designate an origin as having access to everything for that user


## Random Notes and Sketches

### Shell/Result design cleanup

* Extract `GimbalWM` from `client/index.html` into `client/lib/gimbal/gwm.js` — expose as singleton `gwm` in eval scope
* Expose `gsh` (GimbalShell) in eval scope; `gsh.glob()` returns JS array, `gsh.whatever()` returns JS objects
* Add `js()` / `jsl()` terminal methods on Result — `.js()` returns `Promise<any>` (JSON decode), `.jsl()` returns `AsyncIterable` (JSONL line iterator)
* Rename `.toJS()` → `.js()` on Result (drop `.toJS()`)
* Add `json()` / `jsonl()` shell commands (start of pipeline, JSON-encode args)
* Refactor the `with` proxy into a dedicated `Scope` class separating commands, builtins, user vars, and gsh methods
* Port `glob()` from a bare eval identifier to just `gsh.glob()` (keep `glob` alias until proxy is cleaned up)
* Add `xargs()` command for pipeline-to-argument-list pattern
* Add `lib()` loader — convention `lib/<name>/lib.js`, inject Grits `fs`, support `gsh.lib('git')`
* Vendor code: define convention (`client/vendor/<name>/` + `bootstrap.sh`), submodule or script
* Git integration: vendor `isomorphic-git` as `client/vendor/isomorphic-git/`, wrap as `client/lib/git/lib.js` with Grits `fs` backend; `gsh.lib('git')` returns the adapted isomorphic-git API



### Vendor dependency strategy

* Use `npm` as a pure dependency resolver (not a build tool)
* `client/vendor/package.json` lists deps (`isomorphic-git`, `codemirror`, etc.), committed
* `client/vendor/package-lock.json` pins transitive deps, committed (for `npm audit`, Dependabot, reproducible installs)
* `client/vendor/node_modules/` — gitignored, populated by bootstrap
* `client/vendor/bootstrap.sh` — runs `npm install --ignore-scripts --no-audit` in `client/vendor/`
* Wrapper libs (`client/lib/<name>/lib.js`) `import()` directly from `../vendor/node_modules/<pkg>/` using native ESM
* `gsh.vendor_lib('isomorphic-git')` returns the raw library; `gsh.lib('git')` returns the Grits-adapted wrapper on top
* Verify isomorphic-git's ESM bundle uses relative (not bare) imports for its deps (`ignore`, `pako`) so browser native `import()` resolves correctly
* Medium-term: support swapping vendor packages with git checkouts via `client/vendor/overrides.json` (map pkg name → git URL + ref). `bootstrap.sh` applies overrides after `npm install` by replacing `node_modules/<pkg>` with a shallow clone. Stable refs (tags/commits) skip re-clone if already checked out, preserving local dev edits.
* Auto-generate import map in `index.html` at `make deps` time — scan `node_modules/` for bare specifiers, write `<script type="importmap">` block so it stays in sync without manual edits



### Build tooling

* Flesh out Makefile targets: `test`, `install`, `lint`, `fmt`; maybe `run` for dev-server shortcut
* Decide whether `build` should depend on `vendor` or if they should be separate invocations
* Set up CI (GitHub Actions?) to run `make build` on push



### GUI destination

* We need an icons pack
* Each widget has a list of actions: Each has an icon, a snippet of code which defines what happens when you click, and an optional "enabled" state function.
* You can edit the actions for the running widget, you can save it persistently for the current widget, you can define a profile-style override for all widgets of a particular shell command, you can put in a specific new list when you spawn a widget if you like
* The function has w, gsh, and gwm accessible. evalContext maybe for that?
* copy(), paste(), edit(), functions to spawn or access existing opened widgets
* open() is a shell command where double-click or "ok" is an action
* eval() is an action (on selected text) from within the shell
* And so on

### Specific more detailed plan, acme-style

**Overall philosophy**
- Actions are short JS snippets; power is opt-in, surface is familiar
- Inspired by Plan 9 acme (action bar, mouse chords) but applied system-wide unlike acme which is editor-specific
- Everything bottoms out in readable text; no hidden magic, no object model to learn

**Action definitions**
- Each action is `{ icon, label, code }` — icon can be emoji, Tabler icon name, SVG string, or short text fallback
- Icon resolution: explicit icon on action → central registry by name → generic fallback; never breaks
- Arguments use sigil suffixes: `$arg` = exactly one, `$arg+` = one or more, `$arg?` = optional (use selection if present, omit if not, never prompt)
- Actions defined per-widget as a JSON array; same format for built-ins and user-defined so they're readable/copyable

**Eval environment**
- `new Function('gsh', 'gwm', 'self', ...resolvedArgs, snippet)` — no `with`, closed scope
- `gsh` — shell: `eval()`, `cwd()`, shell commands
- `gwm` — window manager: `toast()`, `addWidget()`, `removeWidget()`
- `self` — current widget: `self.save()`, `self.selection`, `self.title`, etc.
- Available names are documented and fixed; no implicit globals

**Action bar / toolbar**
- Each widget has a bar of icon buttons for its actions
- Right-click menu shows the same full action list
- One protected wrench slot always opens the action editor; cannot be removed
- Action editor: action list becomes editable text, toolbar collapses to checkmark only, checkmark evals and reinstalls

**Mouse behavior**
- Left-click: select
- Ctrl + left-click: add to selection (only valid for `$arg+` parameters)
- Middle-click: execute word under cursor, or selection if present; triggers assembly if args unresolved
- Shift + middle-click: hang execution, leave toast open for editing before running
- Right-click: full action menu

**Argument assembly and toast UI**
- Toast appears at bottom when action has unresolved arguments
- Shows command assembling live: `mv("/home/user/a.txt", …)`
- Clicking any file/item in any widget fills next required argument
- Typing fills current argument as text value
- Ctrl during assembly: builds a list for `$arg+`
- Shift or Ctrl-list or edited text: go arrow appears at right
- States: assembling (no go arrow) → hung/ready (go arrow, editable, distinct border) → executing (spinner) → success (checkmark, fades) → failure (red `!`, error and output shown, pinnable or promotable to widget)
- Toast also shows resolved command on success so user can see and learn what ran

**Selection and argument values**
- All values are strings or lists of strings; no richer types
- Clicking a file in the file browser passes its fully-specified path
- Clicking the cwd display in a terminal prompt does the same
- Middle-clicking in terminal executes word under cursor or selection via `gsh`

**Deferred / noted for later**
- Middle-click while assembling: run clicked text and pipe output as current argument (shell `$()` as mouse gesture — expressive but complex)
- Terminal capture of last-run command (toast is sufficient for now)
- X11 middle-click paste compatibility (use explicit `paste()` action instead)
- Regexp matching on action names for icon registry (probably too magic)
- Per-instance vs per-type action serialization strategy


## Old TODOs

* Remote mounts of a volume on a different server
* Save and restore config, instead of using a config file
* Security for endpoints, only allow certain characters in main name / identifier things

* Switch to mmap instead of stream file I/O for blobs
* Chunking of files
* Useful file metadata (owner, file mode, timestamp), make "type" not numeric
* Chunked directories
* fsync()
* Directories show "size" as the number of files underneath them
* Health check, do quick diagnostics to make sure things can work (via an endpoint ideally)
* List of semi-urgent refactorings to FUSE mount module
* Debounce multiple writes into a single consolidated change to the tree
* Performance - e.g. `gatsby new ecommerce-demo https://github.com/gatsbyjs/gatsby-starter-shopify`
* Do a rough security audit of the whole codebase
* HTTP3
* Figure out something for octal modes in metadata
* Switch over client freshness check to whole directory, and make timestamp handling better so we don't reload always on server restart
* Clean up Volume API
* Unify path normalization in GritsClient and SW
* Make hash verifications in client configurable
* Rate limiting and some level of DDOS or malicious request resistance
* Graceful reloading of config file
* Terminal "control panel" showing pertinent information and letting you issue commands
* Make validation of blobs / reference counts when you load, instead of leaking references and thus blobs
* We could communicate client config information (e.g. the list of mirrors) via the volume storage itself
* Fix infinite loop in GritsClient.js, of trying to fetch when the server goes down
* Fix duplication of size limits and cleanup between BlobStore and BlobCache
* Make templating of the JS exports cleaner
* Mandate some limits on what characters are allowed in things (volumes, peerNames, etc)
* Need to worry about key expiration for the certbot keys
* Standardize on URL format for identifying peer and mirror protocol/hostname/port
* Clean up naming, capitalization, consistency issues
* Get rid of NameStore.Link()
* Transition some names from what used to be the "new" approach, to just be the normal name (e.g.
  LinkByMetadata() -> Link()).
* Make mount module able to mount a subdirectory of a volume
* mount module needs automated testing
* Switch some durations to use time.Duration in config files and etc
* Get rid of FileNode.Address()
* Make testbed keep track of errors and indicate success or failure more accurately
* Fix testbed, doesn't seem like it passes when started from clean
* Make HTTP API more best practices (particularly how it returns errors)
* Make test cases for validation
* Make FUSE mounts process NodeForgetter and remove stuff from the inode cache when it happens
