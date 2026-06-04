# Punch list of deferred work right now

## shell

__[n] reuse with Results — using the same history entry twice consumes the Response stream. Fix: buffer to bytes on storage, __(n) wraps in fresh Response on access.

isVoid FIXME — currently checks null and undefined in addition to VOID, which was supposed to be temporary.

cwd FIXME — empty string from backend needs to be treated as root, currently patched in cwdLabel.

Ctrl-Enter statement mode — full JS with let/const/function declarations persisting via a scope object, shared with normal Enter mode.

__[n] reuse index — when you reuse a Result from history in a new chain, it should claim a new __ slot for the new result rather than overwriting the original.

test() currently changes directory as it's running

Pass actual column width to _display() for correct pretty-print wrapping

Shell history arrow-key reliability — most recent command not always at end

Think about JSONL

Think about globs

Think about ..

Make rm() and rmdir() (at least) a lot more simple into a single multilink() (maybe after a quick check first)

bg()

.null()

time()

Line the result __[n] up with the output, not the input

Change :volume to //volume

Sync up the /lib directory with a particular snapshot

Handle binary or very-large files a little better

Detect un-handled options and give an error on those

Fix problem where glob() will return .. which will then get re-resolved

Make editor "Save as" and editing of scratch files more sensible

Make rm without {r:1} not remove directories

Fix double-import (absolute via libUrl vs relative via ../)

## gwm

Syncing files between files() and cd() in a shell optionally

Hotkeys

Click in input field allows text selection without stealing focus

Hook files widget to start at a non-root permitted path

Make multi-file editing based on clicking the title to see a listing
(and same for files widget)

## grits client

Blob volume

Look into possible prefetch loop issue

Add index.html to path lookups pre-emptively

Hash all SW stuff and dependencies separately from /lib/serviceworker so we don't keep reloading whenever some minor client thing changes

Change GritsClient to be per serverUrl

## backend

User module stub

Read .grits/access.json for real access control

Thread user and origin through HTTP call chain

Wire access control to user/origin

Great renaming of concepts

Resurrect service worker

Undo

Prune for path access on multilink

Make multilink (esp with assertions) atomic, with OCC probably

Change "type" to "module" in modules config

Change LookupResponse to just include metadata inline

## Or, for another perspective

### This weekend

Permissions structure (partial root handling, access.json parsing)
Basic users + session tokens (cookie with username + JWT)
Consolidate storage into single root volume
First cut of tracker app wired up end to end

### The demo story

User hits site → redirect script checks cookie → bounces to personal fork if exists, canonical if not
User can open Gimbal environment, navigate the filesystem, edit their copy of the app
Dark mode example works end to end
Tracker app works as a real thing you can point at a directory

### Deferred cleanly

Federated identity (username@server)
SSO across domains
Unicode usernames (ASCII only for now, revisit later)
Full permissions sophistication beyond the basics