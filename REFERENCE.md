# Reference Docs

This software is two codebases which cooperate:

* **Gimbal** is the frontend which provides a Unix-like shell and that "window manager" shown in the examples.
* **Grits** is the backend, the server that provides a read-writable Merkle tree with useful primitives for sharing and replicating content. More or less, it is the filesystem, and Gimbal is the desktop environment.

`grits` is the server piece, the Go code. Gimbal is the stuff that lives in `client/`, the Javascript code, which gets copied into the Merkle tree store to bootstrap the in-browser application system.

## Overall Design Philosophy

There are a few overarching goals that define a lot of the shape of the system:

* **Everything from the frontend**. As much as is feasible, any file that defines operation of the system should be within the frontend's file store so that you don't have to drop back to the backend or `ssh` into anything to change things. The backend reads its configuration from the same place, so things aren't awkward to configure from a server admin perspective.
* **Convention, not configuration**. User home directories are `/home/{username}`, vhost content is in `/sites/{hostname}/live`. Shell commands are enabled by creating a file within the Gimbal vhost called `lib/{command}/main.js`. And so on. We don't do configurable layers of indirection where we can avoid them; we just define where things should be located and then obey the convention on both sides.
* **JSON for everything**. Either JSON or JSONL is used as the universal format for the system's metadata and configuration.
* **Use familiar names**. We imitate Unix commands and filesystem organization as much as is sensible, to avoid forcing people to learn new models or new names for things.

The overall goal is that the system is transparent, and comfortable to interact with and modify.

## Filesystem Hierarchy

Grits can maintain multiple "volumes," and there is even a vague early attempt at implementing the ability to use volumes remotely from some other Grits server. Generally speaking, though, you will use only a single, local volume, `primary`.

Files can be referred to via `//{volume name}/{path}`. You may occasionally see some tools use this double-slash syntax; most of the shell tools should work on files outside of `//primary` if you prefix the volume name with two slashes.

Directories of note within `//primary` are:

* `/home/{username}` - Home directories
* `/home/{username}/local/{app name}` - Intended for application-specific data (private per-user data)
* `/var/{app name}` - Application-specific variable data (public between users)
* `/sites/{hostname}` - Development space for {hostname}
* `/sites/{hostname}/live` - Public web space for {hostname}
* `/sys/etc` - System configuration

Placing shared data for an app into `/var/{app name}`, with appropriate permissions set up, will allow users to contribute data to a communal shared data area while still keeping their own data separated. One structure is to define global `read+insert` permissions on `/var/{app name}`, which means each user's instance of the app can make a directory there for that individual user's contributions, as well as reading the contributions of all the other users.

`/home/{username}/local/{app name}` is designed for a simpler data model, where the app simply has private data on behalf of each user that's going to run the app.

`..` works, but it is a shell thing. The filesystem itself doesn't interpret that filename as special in any way.

There are no symbolic links or Unix-style hard links. The `cp` command creates copy-on-write snapshots — the source and destination share content until one is modified, at which point only the modified side forks.

You can use `gimbal.glob({pattern})` to get a list of files matching the pattern. It's not a "shell command"; it's just a normal async function, so you will do things like `gimbal.rm(... await gimbal.glob('*.txt'))`.

## Permissions

Note: **Permissions are enforced only at the namespace level — blobs are stored in plaintext and served to anyone who has the CID**. Don't store anything secret in this. In particular, content with few possible values can have its CID guessed, so this can bite even where you didn't think you were storing a secret — a PIN or password check can leak this way. (Real confidentiality is a someday-feature via client-side crypto, not CID secrecy.) The system does not have known or obvious ways to violate either its read or write security, but also does not attempt to provide strong read-secrecy guarantees for genuinely sensitive data.

That being said, the overall nature of the system is:

* The filesystem enforces permissions at the directory level. Files have no permissions setup except that for the directory they're in.
* The default is no access. Permission must always be explicitly granted from somewhere.
* Somewhat as a consequence of the Merkle tree structure, **grants of access also apply recursively to directories below the directory where they are granted**. There is no way to revoke a permission on a subdirectory, if a parent directory has it. This is fine, if permissions are granted according to those principles, but it will be surprising and some common practices from Unix security will not work at all here.

Grants of access are also, crucially, specified according to both the user and the origin (the vhost which the user is on). The standard "global" login will log you in as a particular user across all subdomains -- once you're logged in, you are logged in across all apps. But, in general, a random app holding a user's token will still have access to nothing at all except what its origin (i.e. its app) has been specifically granted access to. So it is safe to visit some random subdomain while you are logged in as yourself and let it have your auth token.

In order for that to be safe, of course, the permissions must be set up right. Generally speaking, there should only be two broad grants of access on the system:

* Superuser access on `/` to `glenda` via the `gimbal` origin, and
* User access on `/home/{whoever}` to `{whoever}` via the `gimbal` origin

Any other grants of access should be very specific leaf-type access grants for particular applications from their particular origins. For example:

* `/home/moise/local/music-player/` could be read/writable by `moise` via the `music` origin.

That construction means that, running the Gimbal shell, `moise` can examine the music player's application data and make changes to it. The music player can operate on its own data however it will need to. However, even though it carries a token for `moise`'s user, the music player cannot read or write anything from `moise`'s files outside `local/music-player/`, nor can any other code that doesn't originate from `https://gimbal.{your domain}.com`.

Now say that `moise` wants to set up his own custom Gimbal shell on a different vhost, `https://gimbal.moise.{your domain}.com`. He adds a grant of access on `/home/moise` to his own user when accessed from that new origin. Both vhosts can now access the same data. No other origin can access `/home/moise` unless specifically allowed.

The key factor here is that any random person can code up `music.{your domain}.com`, create a vhost for it, and it will be safe for `moise` to go to that page and interact with it, and it'll have precisely the permissions it should have without being able to read or write things that it shouldn't.

There are other more sophisticated access patterns possible. Notably, all users might have `read` and `insert` permission in `/var/music-player`, meaning that they can all write data to that location to build a shared state, but no one can interfere with other users' parts of the shared state.

### `access.json`

These grants of access come in plain files located at `.grits/access.json` within any directory which permissions are set for. Look around at `access.json` files in `content/skel/` to get a sense of how they're structured; they are simple.

The full set of access types is:

* `read`
* `read+insert`: You may read all data in this directory, but you may not modify this directory or subdirectories. You can *insert* files only (create links which did not previously exist). You can either use this to create an append-only store, or you can use a pattern where users make populated subdirectories which come with grants of access for those users only, so that each person has their own read-writable store and can read other users' stores but may not interfere with them.
* `insert`: Same, but write-only. No user may read back the files which have been appended inside.
* `read+write`: You may read or write any data in this directory *except* that you may not modify the `.grits` directory in it. (You may modify `.grits` directories in subdirectories). This can be used for example to give someone read/write access to a directory without letting them lock you out of it by removing your own permissions to it.
* `owner`: You can read or write anything in this directory, and control permissions by modifying files in `.grits` (including the access control file).

The way that permissions at one level apply to subdirectories below that level falls out as a natural consequence of being able to make reads or writes to the Merkle tree at that exact specific level. Mostly, they simply carry down recursively, but `write` turns into `owner` at lower levels, and `insert` does not itself allow any modification at lower levels. This means you must be careful to fully set up the directory you're adding to an `insert`-only directory, and then `mv()` it into place with permissions already set up such that you'll be able to modify it going forward -- otherwise it will become immutable to you when it's moved out of your home directory, or wherever else you are editing it, and into a place you don't have broader permissions to. See the cloned vhost setup example from [DEMO.md](DEMO.md) for an example of how to do this.

Use `path.allow()`, `path.deny()`, and `path.access()` to manage directory permissions. Run `gimbal.help('allow')` for details on the grant spec and likewise for `'deny'` and `'access'`.

Note that the `origin` is such a critical piece of this security that it *must* be specified with any grant of permissions. There are several ways to specify an origin:

* **Anything without a scheme** (no `http://` or `https://`) is expanded to a subdomain of the core vhost domain. For example, `music` becomes `music.{your domain}.com`, and `myapp.myuser` becomes `myapp.myuser.{your domain}.com`.
* A **full URL** (`http://...` or `https://...`) is used as-is.
* `"*"` grants access to *any* origin. You probably don't want this. It's wrong outside of a couple of specific use cases. You probably want either `"gimbal"` (for granting universal access to "the human,") or else the same vhost where you're putting the content you're modifying access to (for granting access to an app's files specific to that app).

Also note: All of this restriction based on origin is to defend the user, and their **honestly functioning** browser, against a maliciously coded vhost. Do not construct a security measure by assuming that an origin means the browser is running exactly the code you provided. We are defending honest browsers against malicious vhosts, not the other way around -- you can't grant broad access from a particular origin, and then assume that users from that origin will be using the security measures you wrote into the client code for that origin. Things don't work that way.

## Authentication

Auth has two layers: global (persistent cookie) and per-session (tab-local). By default, `gimbal.login('user', 'pass')` logs in just the current tab. Pass `{g:1}` (e.g. `gimbal.login('glenda', 'pass', {g:1})`) to also set a persistent cookie so new tabs pick up the login automatically.

The server checks the tab's session header first, then falls back to the global cookie. This means you can have a global identity (cookie) while a specific tab runs as a different user (session header).

We imitate Plan 9 by defining `glenda` as the superuser account. She has no particular privileges, aside from `owner` permission on the entire filesystem. For privileged filesystem operations on the frontend, do `gimbal.login('glenda')`, then `gimbal.logout()` when done, and optionally `gimbal.login(normal_user, {g:1})` after to restore your global identity. It's a little clunky but it works. You can also open a separate tab for your `glenda` login and just do a non-global login from that tab, and close it when you're done.

Operations done within the backend (for example, actions taken on a FUSE mount of a volume) take place with "backend" permissions, which is an auth-bypassing level higher than `glenda`'s.

Use `whoami()` to see who you're authenticated as.

At present, you can only be authenticated as one user at a time, although a session login can shadow a global login and give you auth as a different user.

## Command Shell

The shell the system presents is basically just a Javascript console with a few added features.

You can type javascript:

```
> 1+1
2
```

Generally speaking, you want to access everything through a root object, `gimbal`. This is also available, outside of our command shell, as `window.gimbal`.

```
gimbal.login({guest:1})
gimbal.whoami()
```

These commands are dynamically dispatched, and can be chained together:

```
gimbal.upload().to('/home/moise/hello.txt')
gimbal.p('/home/moise/hello.txt').read()
```

Pretty much all of these operations are asynchronous. To prevent the syntax from being tortured, we let you chain results together as in the common JS idiom, with the "result" of each feeding in as input to the next, and the command shell will automatically `await` for them all and then return the ultimate result.

That "`self`" variable that's getting passed down will generally either be a `GimbalPath`:

```
gimbal.home().p('foo').mkdir()
gimbal.home().p('foo/bar.txt').write('Hello again')
gimbal.home().p('foo/bar.txt').read()
```

Or a `Response`:

```
gimbal.upload().to('/home/moise/hello.txt')
gimbal.p('/home/moise/hello.txt').read().to('/home/moise/another.txt')
```

These commands are dynamically dispatched (although not every one applies to any possibility for `self` -- `login()` must be called on the central `gimbal` object, for example, and `mkdir()` can only be called on a `GimbalPath`.)


### Paths

Mostly, the commands you'll be typing will operate on paths (`GimbalPath`):'


```
gimbal.site().ls()
gimbal.home().p('hello.txt').write('Hello, world')
gimbal.home().p('hello.txt').read()
```

`.p()` is an alias for `.path()`; that is the operation to access a path relative to some other path. `gimbal.home().p('foo').p('bar/baz.txt')` is conceptually equivalent to `$HOME/foo/bar/baz.txt`.

When it is sensible, you can also give a string argument to to some commands, in which case it'll be taken as relative to the path you called the command on. These are equivalent:

```
gimbal.home().mkdir('src')
gimbal.home().p('src').mkdir()
```

And, commands which take a source and a destination work likewise. For example:

```
gimbal.home().mkdir('backup')
gimbal.home().p('src').cp('backup/src')
```

... will make a backup copy of `$HOME/src/` in `$HOME/backup/src/`.

Note a subtle point -- a path doesn't have to exist for us to operate on it. We create the object for `$HOME/src/` in one of the above examples before that directory exists in the file store. Paths exist independent and separate from files, and they also don't move along with the files when the files move.

Directories of note are:

* `gimbal.home()`, your home directory
* `gimbal.site()`, the webroot of the current vhost
* `gimbal.root()`, the root of the entire directory (which, because of permissions, you will be unlikely to be able to read).

### Copy-on-write

Note that `.cp()` is very different from the Unix command `cp`. It does a copy-on-write Grits link, which means it is a cheap operation even on very large directories, and naturally works on both files and directories.

Similarly, `.rm()` and `.rmdir()` can cheaply throw away even large directories -- you can use directories within this system a little bit like git tags, to store a snapshot or fork a copy of a large directory, as opposed to expensive operations which must move data around proportional to the size of what you're working with.

### Async

As mentioned, the shell will `await` automatically for the results of what you type before returning, to avoid forcing you to type an `await` with every single command. However, if *you* are making use of some intermediate results, you must await within the command:

```
home = await gimbal.home()
home.p('src').cp(await home.p('backup/src'))
```

### Dispatch

Commands are implemented as modules in `lib/<name>/main.js`. Each exports `invoke(prev, ...args)`. Calling `gimbal.xyz()` or `path.xyz()` dispatches to `lib/xyz/main.js`. Commands return plain values (strings, arrays, objects, GimbalPath). The terminal auto-awaits `GimbalResult` values and displays the result.

You can look over `/lib/` within a Gimbal install to get a sense of what commands are available.

## Web Hosting

From the Gimbal shell, you can move stuff into `/sites/{hostname}/live` and it'll show up in the root of `https://{hostname}/`. You can also make arbitrary new directories under `/sites` which will effectively make new vhosts (assuming you've arranged for DNS for whatever vhost to point to your server). Note that the first access will fail, while it's fetching the certificate for you. Wait a few seconds and try again, and it'll work going forward after that.

It's recommended to set up a DNS wildcard for `*.{your domain}.com` so that arbitrary vhosts will work, one per "app" context or user-customized app.

### Service Worker

A lot of the value of this approach gets realized when there is a service worker doing Merkle-tree-aware optimization of when we actually need to be talking with the server. There is one written, in the `serviceworker` module, but it still needs a little work on how it does permissions, and its auto-updating ability and lifecycle, before it's completely production ready.

### Mirrors

Another of those half-working subsystems is a mechanism for sharing out blobs to a mirror network so that other nodes can assist with the load of serving the Merkle tree. It's in module_mirror.go, module_origin.go, and similar. It should be pretty straightforward, it worked as a test version at one point, but it hasn't been looked at in a while and probably needs some resurrection before it'll be useful again.

## Grits Volumes

### Storage Format

All the data is stored as blobs within the Merkle tree, with metadata in JSON format. File identifiers (analogous to inode numbers, or to CIDs within IPFS) look like this:

```
QmcdHV2KQTu59Mq5Aw5jDJXN2CZGKoW68G8pc7jJPvbQ1C
```

Fetching that blob will give you metadata:

```
{"type":"dir","size":61,"contentHash":"QmbjmTt4aJL6p6dZRZMhNomNo3nBaDdoQCGRnk9DrFBMJw","mode":493,"timestamp":"2026-04-16T19:29:57Z"}
```

"type" may be "dir" or "blob"; the content for a blob is just the contents of the file, and the content for a directory is a map of the filenames to their own metadata hashes. In this case, the directory has only one file; reading QmbjmTt4aJL6p6dZRZMhNomNo3nBaDdoQCGRnk9DrFBMJw will yield:

```
{"dest.txt":"QmWxZmx2Ej4BfUCEBSChn8p2NiLy1DGSg3zqB3othfktVL"}
```

And QmWxZmx2Ej4BfUCEBSChn8p2NiLy1DGSg3zqB3othfktVL will contain the metadata node for the file in question:

```
{"type":"blob","size":5,"contentHash":"QmRN6wdp1S2A5EtjW9A3M1vKSBuQQGcgvuhoMUoEz4iiT5","mode":420,"timestamp":"2026-04-16T19:29:56Z"}
```

And so on.

### Operations

There are four main operations available on a Grits filesystem.

* **get(cid)** will return the bytestream for a given CID.
* **put(cid, bytes)** will insert data for a new CID into the blob store, if it is not already present.
* **lookup(path)** resolves a path within the namespace.
* **link(path, cid)** defines a path to point to a particular metadata CID, overwriting any previous value. Linking to
`nil` or `""` will delete the given path, removing it from its parent's directory listing.

`lookup` and `link` will do a good bit of looking around in the blob store to execute; you could do that remotely also (by fetching blobs to walk down the tree manually), but that would be very slow. Lookups, in general, will return the LookupResponse structure from `namestore.go`.

(You need to link a blob which you did `put()` on into the namespace pretty quickly, or it will be deleted. Generally speaking, to write content, a client will try to do a `link()` to a piece of storage which may not exist on the server yet, and the server will repeatedly report which blobs it is missing in order to be able to process the link, and the client will upload them, then rinse and repeat. But, if there is no successful link to the content within a few minutes after a `put()`, the server is free to delete the unreferenced blob.)

### Atomicity

Both lookup() and link() take any number of paths in their argument, and link() allows you to make assertions about the current state of the filesystem (for example asserting that a path you are writing is `nil` previous to the write, if you want to guard against overwriting a previous entry). This combination allows for atomic operations. In particular, you can serialize big operations on the file store via an OCC approach: First gather the existing state of what you want to modify, then execute a write which asserts that everything you're modifying has the value you previously observed. Then, on an assertion failure, re-read and try again until the assertion succeeds, which means the write has finally succeeded.

### Notifications

Another thing which falls out of this naturally is watches for modification -- simply keep an eye on the CID of any file or directory node, and if it changes, it's been modified.

### Performance

The remote performance is pretty mediocre right now. It should be possible to do prefetching more cleverly than we're doing now. Also, when configured to (when concurrent access isn't expected), we should batch up writes into long lists of write-back-cached "path/CID/assertions" tuples which we are committing from a background thread. That should make it better. But, for now, it's not super fast when doing I/O remotely with writes involved.

Reading (serving the read-only bits of the site) should already be faster than a normal web site, if the service worker is enabled. Try it out, see if that's the reality.

## Code Layout

Generally speaking, the client code in `client/lib` is meant to be easy to hack on, and it's expected that average users will be going in and making customization if they are capable. The Go code that runs the backend is meant to be just a general support layer, not needing lots of custom changes, but if you are curious about the organization of the backend code, these are the main things to understand:

### `internal/grits`: Core: Blob storage, configuration, the namespace, and so on. 

* `structures.go` defines core interfaces used throughout
* `blobstore.go` defines the blob storage (mapping of content hash -> actual content)
* `namestore.go` defines the mapping of path names to content hashes

### `internal/gritsd`: Server Implementation

* `server.go` defines the actual server implementation.
* `modules.go` defines an interface for "modules" added to the server. Almost all the server's functionality is implemented via these modules. You will see many of them; of particular importance the ones listed in the next (config.json) section.

### `config.json`

This is the core config file. Most of it involves configuring particular modules. There are a decent number, in a variety of states of working-ness, but you can see a quick sample in the sample config. Some among them that may be useful and somewhat-work right now are:

* `volume` provides a local writable volume of storage
* `mount` creates a FUSE mount of a particular volume to a local directory
* `http` creates an HTTP endpoint providing access to the API
* `serviceworker` sets up a service worker which will run all web accesses through a client-side Grits cache automatically, so these benefits apply to any content. (Note: Currently the SW works on Chrome, but not completely on Firefox; for what reason, I don't know.)
* `startup` provides a list of backend commands to run on startup. See the next section.

You can check out the source in `internal/gritsd/module_{whatever}.go` to see the configuration structure for each.

### `var/`

`var/` is the area for writable data used by the server in operation. It should be fine (and would be recommended) to have the entire `grits/` directory outside of `var/` owned by a different user, and read-only from the POV of the server process.

### `client/lib`: Frontend

This is automatically imported to the Grits store on every server startup. Each tool goes in its own directory here. `main.js` in a tool directory means it is runnable as a shell command. `gwm-widget.js` in a tool directory means it can be opened as a widget in the graphical shell.

There is also a `client/lib/node_modules` directory, which is populated by `make deps`.