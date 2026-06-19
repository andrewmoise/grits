# In Progress

* Self-hosted git repo in startup
* MD renderer
* MOTD
* Fix certbot browser badness when first accessing a new vhost, and browser visits http://{whatever}



## Shell/Result design cleanup

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



## Vendor dependency strategy

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



## Build tooling

* Flesh out Makefile targets: `test`, `install`, `lint`, `fmt`; maybe `run` for dev-server shortcut
* Decide whether `build` should depend on `vendor` or if they should be separate invocations
* Set up CI (GitHub Actions?) to run `make build` on push



# Backlog



## shell

__[n] reuse with Results — using the same history entry twice consumes the Response stream. Fix: buffer to bytes on storage, __(n) wraps in fresh Response on access.

isVoid FIXME — currently checks null and undefined in addition to VOID, which was supposed to be temporary.

cwd FIXME — empty string from backend needs to be treated as root, currently patched in cwdLabel.

Ctrl-Enter statement mode — full JS with let/const/function declarations persisting via a scope object, shared with normal Enter mode.

__[n] reuse index — when you reuse a Result from history in a new chain, it should claim a new __ slot for the new result rather than overwriting the original.

test() currently changes directory as it's running

Pass actual column width to _display() for correct pretty-print wrapping

Shell history arrow-key reliability — most recent command not always at end

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



## shell tools

facl() supporting directory recursion up and down

signup()

cwd() shell command

Import git tools libs

Defaults / config system

More tools: Diff, patch

adduser() and deluser() from frontend

~~Make skel() function~~

Make editor "Save as" and editing of scratch files more sensible

Make rm() and rmdir() (at least) a lot more simple into a single multilink() (maybe after a quick check first)

bg()

Make test() in the foreground once bg() exists

.null()

time()

js() and json() (replacing all the .toWhatever() methods that aren't quite exactly right)

Make more secure login self test than test/test user

Make test() print failures as red, or a red X or something is probably easier



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

Distinguish user input from JS output, in gterm

Make "up" not discard the line we're currently working on, in gterm

Make the index.html title correspond to the vhost



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

What's up with sessionStorage in GritsClient.js?

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



## backend

Be smart about sending small blobs directly with the answer, for stuff like lookup(), to save RTT

Transition a bunch of stuff to JSONL

Great renaming of concepts

Finish service worker

Undo

Prune for path access on multilink

Make multilink (esp with assertions) atomic, with OCC probably

Change LookupResponse to just include metadata inline

Make more complete auth-timeout-force-re-login system (an endpoint to verify user-initiated action, and a way to configure auth timeouts per user)

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

Change access.json to JSONL syntax

Check a little more thoroughly that multiple auth tokens at once work well

Eliminate /grits/v1/content

Make paths in APIs require an initial slash maybe

Make systemctl setup

Make a monitor for /sites/ that gets certs right away when a directory is created



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
