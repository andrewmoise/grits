# A framework for web-native development

This is a little framework for development of self-hosted and/or community driven web applications.

It's still a work in progress. **This code is not production ready or close to it.** Some things work in useful form, but also it's still heavily in flux. This is mostly just a demo of the intended direction of the system, for people to mess with and give feedback.

Some of the things it is designed to make easy are:

* For users to examine or delete their data on a web app, without needing to go strictly through the web app's interface
* For users to modify their instance of a web app they're using if the existing functioning doesn't suit their wants (or clone/fork it completely)
* For admins to run a lightweight backend which can host many arbitrary apps without needing support from the backend side specific to each app
* For admins and their friends to semi-easily add capacity (storage or networking load) to the server (*)

The system that accomplishes that is split into two cooperating pieces.

* **Grits** is the backend. It's a read/writable Merkle tree with primitives for sharing and replicating content in a distributed environment. More or less, it's the filesystem, with a web server that exposes that filesystem subject to file permissions.
* **Gimbal** is one frontend web app atop Grits. It's a demo of the system, also an allegedly useful system for doing administration and development for other apps which exist on the system.

If you are interested in how to set up your own instance of the backend, see INSTALL.md; if you are interested in a detailed reference of how this all works, see REFERENCE.md. What will follow is quick demonstrations of what the system can do right now.

## Quickstart

Specifically the examples we'll show are:

1. How as an end-user to modify the code of the app you're interacting with
2. How as an end-user to search through data the app is storing
3. How as an end-user to clone an app into a new vhost that you're fully in charge of

Gimbal is the frontend, a vaguely Unix-like desktop environment in the browser. It's easy to try; go to [gimbal.melanic.org](https://gimbal.melanic.org/). You may be there now.

From the terminal widget (the `>` prompt), you can try basic operations. The terminal makes a `GimbalClient` instance available as `gimbal`, and that object serves as the root of your interactions with the system. Mostly, you'll be grabbing paths within a vaguely Unix-like filesystem structure, and doing actions on those paths:

```
gimbal.login({guest:1,g:1})                    // Start a guest session
gimbal.whoami()                                // See who you are
home = await gimbal.home()
home.path('hello.txt').write('hello')
home.path('hello.txt').read()
home.ls()
```

You can learn more about how this works in REFERENCE.md, but hopefully it's pretty apparent the basic gist of what's happening.

### 1. Changing Application Code

So as mentioned, you don't have to be an administrator of the site to change the application code. We'll show a real example.

Gimbal's editor has an inconvenient feature: It doesn't do line wrapping. I haven't fixed that, even though it's straightforward, so it can serve as an example of how end-users can change the code they're experiencing in arbitrary ways.

Observe. So up above, we defined a variable `home` for our home directory; now we're going to get another one we'll need a lot, `site`, for the webroot of the current vhost. (You could also access this at `gimbal.p('/sites/gimbal.melanic.org/live')`, but `gimbal.site()` is simpler.)

This will launch a browser of the files within the webroot, rearranging the windows of the little desktop environment as a result. Run this (and then open up `src/README.md` to see the line-wrapping problem we're trying to fix, and then find your way back to these instructions to see how to fix it):

```
site = await gimbal.site()
site.launch('files')
```

See? Should look like this, without line wrapping:

(TODO - screenshot)

Not hard to fix, though.

```
site.p('lib').ln(await home.p('my_editor'))
home.p('my_editor/codemirror/gwm-widget.js').launch('edit')
```

We are using codemirror for editing, and as it happens it's a one-line fix to the adapter code to add line wrapping:

(TODO - screenshot)

Make the edit, hit Ctrl-S, and then use the modified editor:

```
site.p('src/README.md').launch(await home.p('my_editor/codemirror/gwm-widget.js'))
```

(TODO - screenshot)

See? Fixed now. You just changed the site code. But only for you -- the other users are still running the stock Gimbal source (as are you, for now, unless you opt specifically to run this custom widget code -- but see the next section for how to change that.)

The option to use this customized editor will persist across sessions for as long as you're using the guest login.

### 2. Cloning a Site

So that is simple to do (relatively speaking), but leaves your system in a half-and-half state wherein you'll be loading widgets from one cloned Gimbal install into an environment hosted by the ancestor of the clone. That is fine, but also, it'll be useful to have a whole system you can edit without having that chicanery involved. That's actually simpler; it involves some vhost and permissions stuff is the only reason we started with modifying one widget. Basically, you can at any time make a full clone of an app (in this case, the entire Gimbal operating environment) which you can then modify however you'd like.

If you're interested, this is how:

#### Make your local clone

We make a copy of the `gimbal.melanic.org` site onto a new vhost. So, by design this is something that any member of the site can do. Like tumblr. We just need to make a new directory in /sites corresponding to that host. Read the permissions section in REFERENCE.md for more, but the quick version of what you need to understand about how the permissions for this go is:

* Every grant of permissions is described in terms of *both* the user being granted access and the origin (i.e. the vhost that user is on, i.e. the code the user is running.) 
* Permissions are additive as you go down the file tree from the root. You will not have any access to `/sites` *except* for the ability to create a directory in it under a name that doesn't already exist.
* The directory in `/sites/` *must* be created with its permissions already in-place. Because everyone's permissions in `/sites/` are so restricted, you cannot make a directory there and start mucking around in it. You must make the directory, set up the permissions to grant yourself owner access, and then move it into place fully formed.

So with that in mind, the first step is to make a clone of `gimbal.melanic.org` in our home directory:

```
gsh.home().path(`gimbal.anon1895.melanic.org`).mkdir()
gsh.path('/sites/gimbal.melanic.org/live')   \
  .cp('/home/anon1895/gimbal.anon1895.melanic.org/live', {r:1})
```

(There's nothing special about the `anon1895.melanic.org` hostname space or whatever; we're just doing that to keep things organized. You could write literally anything there as the name of the new vhost, as long as it doesn't collide with an existing one. You cannot "claim," in other words, the whole namespace under your specific username, which would yes be a nice thing to be able to do.)

#### Set up permissions

Now the the directory is in your home directory, you need to grant access to yourself to it. We're going to grant access to the new site from both the normal `gimbal.melanic.org` origin, and also the Gimbal shell which will be running at `gimbal.anon1895.melanic.org`. We're planning to be able to use both, which means we want to be able to edit the new site when operating either, in other words.

```
gsh.facl(gsh.home().abs(), {u:'anon1895', o:'https://gimbal.melanic.org/'}, {p:'owner'})
gsh.facl(gsh.home().abs(), {u:'anon1895', o:'https://gimbal.anon1895.melanic.org/'}, {p:'owner'})
```

And, we want to grant access also to our home directory from the new customized shell we're making. Random apps do *not* have access to the home directory even if they have our auth token.

```
gsh.facl(gsh.home(), {u:'anon1895', o:'https://gimbal.anon1895.melanic.org/'}, {p:'owner'})
```

It is worth looking over these `facl()` calls carefully if you want to understand how permissions here work. It's different from Unix. Also, note that `{o:'https://gimbal.melanic.org/'}` for the origin is usually shortened to just `{o:'gimbal'}`, but we spell them out in full here just for clarity.

So at the end of that, we've got permissions set up so that we can edit the vhost as we need. Note that our new `gimbal.anon1895.melanic.org/live` directory, where the actual web root lives, already has world read permissions, because it inherited them when we cloned `gimbal.melanic.org`. Without that, serving our web site wouldn't work for anyone whose browser wasn't carrying our auth tokens.

You can query for the world-readable permissions that we're depending on, for the world to be able to read both of these, if you type:

```
gsh.facl('/sites/gimbal.melanic.org/live')
gsh.facl(gsh.home().path('gimbal.anon1895.melanic.org/live'))
```

#### Make it live

In any case, once you've set up permissions, you can make the thing live.

```
gsh.home().path('gimbal.anon1895.melanic.org').cp('/sites/gimbal.anon1895.melanic.org', {r:1})
```

If that works, then load up:

```
https://gimbal.{your username from whoami()}.melanic.org/
```

... and you should see a whole Gimbal environment:

(TODO: screenshot)

This one is yours, though. Remember the change to editor line wrapping?

And it isn't limited to the editor. From a full clone you can change anything,
and what you end up with is a real, independent site that's yours — not a tweak
layered on top of someone else's.


## Grits Quickstart

So again, Gimbal is the frontend (Javascript); Grits is the storage backend that makes all this function.

They currently come together in one git repo. If you would like to run the backend yourself to host a Gimbal instance, then this is what you do:

### Build

This only works on Linux right now.

Set up prerequisites:

* Install golang >= 1.25.11
* `sudo apt install fuse3 certbot npm` or equivalent

Install and build:

**Note! `make deps` will install `govulncheck` on your system, in order to be able to do a security audit during `make audit`.**

* `git clone https://gimbal.melanic.org/src/.git grits`
* `cd grits`
* `make test` to run backend smoke tests
* `make deps` to fetch some JS and Go dependencies
* `make` to build the server

Also note -- the initial `git clone` is extremely slow because it's using bare HTTP on the self-host instead of speaking to a Go server. I still think this is the best solution (better than depending on Github, and better than coding git-specific things into our backend), but also, it's sure not ideal for it to be so slow.

### Configure

```
cp sample-config.json config.json
hx config.json
```

Change `%USER%` to your Unix username and `%EMAIL%` to your email (only needed
for certbot — the system will grab HTTPS certificates automatically, and certbot
wants your email for that).

### Run

```
sudo bin/gritsd
```

It'll drop privileges to your configured user as soon as it's opened the ports
it needs. If you want to try it as non-root, configure a port above 1024, run
certbot by hand for certificates, and then run without `sudo`.

### Initial setup

Once the server is running, the FUSE mount at `mnt/` gives you access to the
file store.

A note on shutdown: if you shut the server down while things in the FUSE mount
are in use, it'll refuse to finish shutting down to avoid leaving a stale mount.
Close any open files, cd out of the FUSE mount, unmount it manually, and the
shutdown will continue.

A note on the auto-import: the sample config automatically imports `client/` to
the live directory for `gimbal.{your domain}.com` on every server restart. Any
edits to `client/` in your backend checkout will overwrite local changes in the
store. Other vhosts, including user-created copies, are not affected.

Populate the client store with the one-time skeleton import:

```
cp -r content/skel/* content/skel/.grits mnt/
```

Then create users:

```
bin/grits adduser glenda
bin/grits adduser {your username}
```

Create a DNS entry for `gimbal.{your domain}.com`. For a real deployment, a
wildcard `*.{your domain}.com` is recommended — a lot of the power here comes
from being able to cheaply add new vhosts.

### Test

Log in at `https://gimbal.{your domain}.com/` and run the self-tests:

```
gsh.login({g:1})
gsh.test()
```

Frontend tests will take a while.

### Web serving

Once tests pass, you can start putting up content. `gsh.upload()` and
`gsh.path('/path').unzip()` are useful. The copy-on-write version workflow looks like this:

```
gsh.path('/sites/{hostname}/dev/v1').mkdir({p:1})
gsh.path('/sites/{hostname}/dev/v1/index.html').w('version 1')
gsh.path('/sites/{hostname}/dev/v1').ln('/sites/{hostname}/live', {ff:1})
```

(The `ff` option forcibly overwrites `live` with a copy of `dev/v1`, instead of
the normal Unix behavior of creating a new file inside an existing `live/`
directory.)

To start a v2:

```
gsh.path('/sites/{hostname}/dev/v1').ln('/sites/{hostname}/dev/v2', {ff:1})
```

You can deploy `dev/v2` to a temporary vhost's `live/` to test against real auth
and real data before cutting it over.

### Permissions

The permissions system differs from standard Unix in two important ways:

* Permissions are additive only. The root directory starts out denied to everyone;
  grants of access increase as you go deeper into the tree.
* Permissions are framed in terms of both the *user* attempting access and the
  *vhost* they're coming from.

This lets you be very surgical: you can grant a specific piece of code, running
for a specific human, access to a specific directory — without a malicious app
being able to reach things it shouldn't. See REFERENCE.md for the full explanation
and for how to work with `facl()`.

### Backend administration

Launch with `sudo bin/gritsd`. Making it a systemctl service is on the todo list.

Useful backend commands:

* `bin/grits ping` — test that the cmdline module is working
* `bin/grits import local/path //volume/dest/path` — import files from your
  Linux filesystem into the store
* `bin/grits adduser username` — add a user (prompts for password)
* `bin/grits deluser username` — delete a user

The intent is that most administration will eventually happen from inside the
frontend — HTTP logs, user management, that sort of thing. Some bootstrapping
will always require backend commands regardless.

---

## Further reading

* [REFERENCE.md](REFERENCE.md) — full technical reference for the permissions
  system, configuration, and API

## In conclusion

See? It's neat.

Live instance: [https://gimbal.melanic.org/](https://gimbal.melanic.org/)

Matrix: `#gimbal:matrix.org`

Comments, questions, feedback? [Let me know.](mailto:moise@melanic.org)
