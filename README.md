# Grits - A web-native operating system

It's two pieces:

* **Grits** is a backend project which provides a read-writable space with useful primitives for sharing and replicating content.
* **Gimbal** is a frontend project which provides a Unix-like shell — a little "web operating system" — with some nice abilities, native in the browser, which are normally available only on the backend (thus separated from the browser side of the operation.)

These pieces operate in tandem to provide an experience different from a conventional "backend / frontend" structure. This structure enables you to do many things which are useful.

As an end-user of the web site, you can:

* Directly examine the data and code underlying the web site
* Persistently edit the UI of the web site you're using, for you, without impacting other people's experience
* Interact and script interactions with the site in ways not programmed in by the authors/operators

As an admin, you can:

* Quickly test, deploy or roll back versions of the site without it being clunky
* Do first-class maintenance and development things from your browser
* Replicate content to mirrors, and have them seamlessly assist with the load on the main site

It's still a work in progress. It's a hobby project. It works on my machine.

In particular, don't put this live on the internet. There's no permissions control, so either no one can write (which isn't fun) or everyone can. It may eat your data; there are known data-corruption bugs I haven't sorted out yet. Don't use this in production.

## Motivation

The motivation for all this is this: The internet is based on a peer-to-peer and open software type of vision, but at least as far as the modern web, it's still very much stuck in a mainframe-style "priests and outsiders" organizational structure. (It is in fact, for business reasons, going backwards -- fewer and fewer priests maintaining ever more opaque temples for more and more hapless supplicants.) As an end-user of a web site, you're incapable of interacting with it in common ways that are natural if you are an open-source type of person, even in the case when the software is (from an administrator's POV) open source.

And then, on the admin side and compounding those issues, there are significant centralized hosting issues that still haven't gone away. Bittorrent is great, ActivityPub is great, but popular sites still run on expensive centrally-served hosting. People still invest in S3 to run their Mastodon nodes. My vision would be that it becomes realistic to run a busy Mastodon node or Peertube instance, and have a substantial amount of the hosting being done by the users.

The idea for this project is to provide an option which is more akin to minicomputers instead of mainframes, or of open source instead of Windows.

## Examples

Here's what it looks like:

(screenshot - normal environment)

You can see a terminal, a files browser, and an editor. Should be pretty straightforward.

Terminal commands are interpreted more or less as Javascript syntax. You can do javascript things:

(screenshot - evaluating 1+2, defining and calling a function)

But, you can also run Unix-like commands, including chaining them together:

(screenshot - cd(), cat(), echo().to(), upload().to(), unzip(), cd(), ls())

These commands are chained together via Response bytestreams, with function chaining analogous to `|` in Unix. `.to()` is analogous to `>`, `from().` is almost analogous to `<`.

The system is backed on a Merkle tree filesystem, which naturally makes it possible to efficiently work remotely while maintaining strong cache-coherency guarantees. It also means it's easy to make copy-on-write copies of big things for your own editing:

(screenshot - cd(':client/lib', ':home/moise/lib'))

(screenshot - opening a new tab in the new shell)

(screenshot - editing the shell within the shell)

(screenshot - showing the changed shell after a reload)

And so on.

## Quickstart

### Build

It only works on Linux right now, as far as I'm aware.

To play around with it, do this:

* Install golang >= 1.22.12
* `sudo apt install fuse3 certbot` or equivalent
* From the source directory:
    * `go test ./...`
    * `go build -o bin/certbot-helper cmd/certbot-helper/main.go`
    * `go build -o bin/gritsd cmd/gritsd/main.go`

### Configure

```
cp sample-config.json config.json
hx config.json # Or whatever editor
```

You will need to make significant changes to the config. To play with it significantly, you must disable read-only-ness on the HTTP module, but since there are no permissions this will make your data world-writable. Use your own judgement, probably don't leave the service running in production.

The system will automatically grab HTTPS certificates for you. It needs your email to do that because certbot requires it.

### Run

```
sudo bin/gritsd
```

(Note, it'll drop privileges to whatever user you configured for it, as soon as it's opened the ports it needs. If you want to try it as non-root, just configure it on a port above 1024 and set up a manual certbot invocation for its certificates.)

### Test

From the project directory in a separate shell:

```
mkdir volumes/sites/{your server name}
```

(Note: That's modifying the content within the Grits volumes, which in the default config are FUSE-mounted in `volumes/`. You need to make the volumes read-writable for this to work... it is somewhat sensible to set the HTTP endpoints to read-only, and still have the volumes read-writable. But, the main fun of the project at this point is remotely editing files content, so that way would be only if you want to put static content into `volumes/` from the backend and not be able to edit it from the frontend.)

Anyway, once that `sites/` directory is created, the server will be willing to grab a certificate and start serving content for that host. There's no "site content" yet, but you can access the API endpoints directly. In a browser, open:

https://{your server}/grits/v1/content/client/lib/gimbal/

... and it'll show you a little administration interface with a Unix-like shell. Run:

```
test()
```

That'll run you through a little self test.

You can also try populating some "frontend" content.

```
mkdir(':sites/{your server}/content')
echo('Hello!').to(':sites/{your server}/content/index.html')
```

Then open https://{your server}/

Assuming that works, you can start populating content if you like. `upload().to(filename)` and `unzip(filename)` may be useful. Bear in mind that it is trivial to maintain multiple copy-on-write versions of the site content:

```
cd(':sites/{your server}')
mkdir('dev/v1',{p:1})
echo('version 1').to('dev/v1/index.html')
ln('dev/v1','content',{ff:1})
```

(That "ff" option requests to forcibly overwrite whatever's in `content` with a copy of `dev/v1`, without the normal Unix semantics of creating a new file within `content/` if `content/` already exists.)

And then, if you want to work on a v2 of the site:

```
ln('dev/v1','dev/v2',{ff:1})
```

And then, make edits to `dev/v2`, and then you can observe them at `https://{your server}/grits/v1/content/sites/{your server}/dev/v2/`, and then if you like them you can use another `ln` command to deploy them to `content/` which will place them on the "live site."

Hopefully this all gives the flavor of the intended interaction. When you're done, hit Ctrl-C, the server should shut down cleanly. If it hangs because it can't unmount the FUSE mount, just end the processes that are keeping the FUSE mount busy and then unmount it yourself, and the shutdown should continue from there.

And yes it's on the roadmap to make it runnable via systemctl, and file permissions so that not everyone can see `:sites/{your server}/dev`. Both of them would be good to add certainly.

## How It Works

### Filesystem - Grits

The "filesystem" here is specifically constructed with operating principles that are useful to Gimbal-style apps, as well as general file replication operations that are useful for any static content. It is implemented as a Merkle tree, which provides several advantages:

* We can easily tell what does and doesn't need to be updated in our local view of remote content, simply from doing a single fetch of the root CID.
* We can verify content that comes from a semi-untrusted source, which allows us to deploy mirrors while limiting the level to which we need to trust them.
* We can easily incorporate useful features like file history, copy-on-write semantics, and atomic operations, by leveraging the CID as a descriptor of all content below it.

#### Structure

The blob identifiers used are hash values of the content, in the exact same format and compatible with IPFS CIDs. Each file is represented by a metadata node (corresponding to a Unix inode), and they are generally referred to by the CID of the metadata node data in JSON format. For example:

```
QmcdHV2KQTu59Mq5Aw5jDJXN2CZGKoW68G8pc7jJPvbQ1C
```

Might refer to:

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

#### Operations

There are four main operations available on a Grits filesystem.

**get(cid)** will return the bytestream for a given CID.

**put(cid, bytes)** will insert data for a new CID into the blob store, if it is not already present.

**lookup(path)** resolves a path within the namespace.

**link(path, cid)** defines a path to point to a particular metadata CID, overwriting any previous value. Linking to
`nil` or `""` will delete the given path, removing it from its parent's directory listing.

`lookup` and `link` will do a good bit of looking within the blob store to execute; you could do that remotely also (by fetching blobs to walk down the tree manually), but this would introduce significant latency. Lookups, in general, will return the LookupResponse structure from `namestore.go`, meaning that you can skip the intermediate indirection and do one lookup and then immediately load the content blob for the file without further investigation.

#### Atomicity

Both lookup() and link() take any number of paths in their argument, and link() allows you to make assertions about the current state of the filesystem (for example asserting that a path you are writing is `nil` previous to the write, if you want to guard against overwriting a previous entry). This combination allows for atomic operations. In particular, you can serialize big operations on the file store via an OCC approach: First gather the existing state of what you want to modify, then execute a write which asserts that everything you're modifying has the value you previously observed. Then, on an assertion failure, try again until the assertion succeeds which means the write executes.

#### Notifications

Another thing which falls out of this naturally is watches for modification -- simply keep an eye on the CID of any file or directory node, and if it changes, it's been modified.

#### Roadmap

There are some pretty essential improvements which are planned for the near future:

* File permissions and authentication / user management
* Backups and file history
* Fixes to naming conventions, versioning of file formats, new version of file formats

### Shell - Gimbal

The shell that the system presents is basically just a Javascript console with a couple extra features.

You can type javascript commands:

```
$ 1+1
2
```

You can also type some Gimbal-specific shell commands:

```
$ cd(':sites/{your server}')
$ upload().to('test.zip')
$ unzip('test.zip')
$ ls()
["bootstrap-5.3.8-dist","test.zip"]
$ cd('bootstrap-5.3.8-dist')
$ ls()
["css","js"]
```

#### Paths

Volumes are accessible via `:{volume name}/{path}`. That's a bad syntax; I plan to change it to `//{volume}/path` or `//{server}:{volume}/path` at some point soon.

`..` works, but it is a shell thing. The filesystem itself doesn't interpret that filename as special in any way. There are no symbolic links.

You can use `glob({pattern})` to get a list of files matching that pattern.

#### Chaining

The way commands chain is a little complex. Generally speaking, shell commands return a `Result`, which can have functions called on it which goes via the same proxy which found the commands in the first place. Commands are implemented in `client/lib/{command name}/main.js`. See the comments at the top of `client/lib/gimbal/gsh.js` for more about the details of how it all works.

### Web Hosting

From the Gimbal shell, you can move stuff from there into `:sites/{your server}/content` and it'll show up on the `https://{your server}/` web site. You can also make arbitrary new directories under `:sites`, and stuff in their `content/` directories will get served as normal (assuming that you arrange for DNS for whatever host to point to your server).

(You can, if you like, set up a DNS wildcard so that `*.{your server}.com` all points to the server where Grits is running. In that case, anything that gets placed in `:sites/{whatever}.{your server}.com/content` will become a live site automatically.)

## Code layout

### `internal/grits`: Core. Blob storage, configuration, the namespace, and so on. 

* `structures.go` defines core interfaces used throughout
* `blobstore.go` defines the blob storage (mapping of content hash -> actual content)
* `namestore.go` defines the mapping of path names to content hashes

### `internal/gritsd`: Server implementation

* `server.go` defines the actual server implementation.
* `modules.go` defines an interface for "modules" added to the server. Almost all the server's functionality is implemented via these modules. You will see many of them; of particular importance are:

* `module_http.go` for providing HTTP service
* `module_mount.go` for FUSE mounting
* `module_volume.go` for a local writable storage volume

### `config.json`

This is the core config file. Most of it involves configuring particular modules. There are a decent number, in a variety of states of working-ness, but you can see a quick sample in the sample config. Some among them that may be useful and somewhat-work right now are:

* `volume` provides a local writable volume of storage
* `mount` creates a FUSE mount of a particular volume to a local directory
* `http` creates an HTTP endpoint providing access to the API
* `serviceworker` sets up a service worker which will run all web accesses through a client-side Grits cache automatically, so these benefits apply to any content

Check out the source in `internal/gritsd/module_{whatever}.go` to see the configuration for each.

### Other useful directories

* `client/` defines vital client-side content. It gets populated into a special read-only volume on startup, so that clients can always access stuff within it.
* `var/` is where all writable data for the server is kept. It should be fine (and is recommended) to have the entire `grits/` directory outside of `var/` owned by some different user, and read-only from the POV of the server process.

## In Conclusion

See? It's neat. I made a Discord for it, come join the fun, let me know what you think.

(discord link)

## Enjoy!

Comments? Questions? Feedback? Let me know.