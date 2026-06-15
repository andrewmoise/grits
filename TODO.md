# In Progress

* Make the corner icons a little bigger
* Look into service worker not unregistering
* Change "type" to "module" in modules config
* Double-check permissions within .grits directories — the access.json in
  foo/.grits should apply to both foo/ and foo/.grits/ itself, with slight
  differences between the two contexts
* Consolidate client/ and skel/ into a single gimbal/ directory
* Make a more secure way to input passwords for login()
* Fix test /tmp/tmp scratch dir
* Move to $HOME/tmp
* Make a more friendly vhost setup in the instructions
* Session tokens in cookies or something
* What's up with sessionStorage in GritsClient.js?
* Reorient the initial paradigm to vhosts instead of /grits/v1/content
* Make sure only one instance can run at once
* Change "root" volume name to something less collide-y

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

Make rm() and rmdir() (at least) a lot more simple into a single multilink() (maybe after a quick check first)

bg()

Make test() in the foreground once bg() exists

.null()

time()

Line the result __[n] up with the output, not the input

Sync up the /lib directory with a particular snapshot

Handle binary or very-large files a little better in terminal output

Detect un-handled options and give an error on those

Fix problem where glob() will return .. which will then get re-resolved

Make editor "Save as" and editing of scratch files more sensible

Tab completion

Don't add void returns to history

Do away with doHistory

Shell environment variables

Some indication when things disconnect

Import git tools libs

Defaults / config system

More tools: Diff, patch

adduser() and deluser() from frontend

Make skel() function

Fix copy and paste from Gimbal shell

cwd() shell command

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

## grits client

Blob volume

Look into possible prefetch loop issue

Add index.html to path lookups pre-emptively

XX Change GritsClient to be per serverUrl
(Probably superceded now; instead we need to look at having the SW bounce every blob read to immutable accesses to /grits/v1/blob in the core vhost)

s/cell/creature/g

Sort out a way to upload unlimited amounts of stuff without timing out the "PUTs get ref-held for 45 seconds only" issue

Lookup and link multiple things in one request when possible

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
* More coherent error message when a dead previous FUSE mount is in the way
* Fix port 8080 on ACME challenge