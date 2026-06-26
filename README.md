# Gimbal: A framework for web-native development

Gimbal is planned as a web framework that lets you interact with a web site or application much more directly than you usually can. As an end-user of an app that runs on Gimbal, you can:

* Directly examine the site's underlying data (subject to permissions) and code
* Persistently edit the UI of the site you're using without affecting anyone else's experience
* Interact and script interactions with the site in ways not programmed in advance by its authors

As an admin, you can:

* Easily test, deploy, or roll back versions of the site
* Do first-class development and devops from your browser

Note: This is still a work in progress — a hobby project. It works on my machine. **It might eat your data at any time.** The permissions might fail. The service worker doesn't update properly. **Don't use this in production.** Some things work and it's fun for tinkering, but it's pretty far from a working v1.

The overall system is two cooperating pieces:

* **Gimbal** is the frontend — the Unix-like shell and window manager you've been
  using. It's the Javascript in `client/`.
* **Grits** is the backend — a read/writable Merkle tree with primitives for
  sharing and replicating content. More or less, it's the filesystem, and Gimbal
  is the desktop environment. It's the Go code in the root.

## Gimbal Quickstart

You don't need to run anything to try Gimbal. You can probably find a testbed instance to play with at [gimbal.melanic.org](https://gimbal.melanic.org/). You may be there now.

From the terminal widget (the `live $` prompt), you can try some basic operations:

```
pwd()
ls()
whoami()                // Should print nothing; you're not logged in
login({guest:1,g:1})
whoami()                // Now should print the throwaway login that was created for you
cd()
pwd()                   // Should print your home directory
echo('hello').to('hello.txt')
ls()                    // Should see hello.txt now created
```

### Changing the code

As it happens, the Gimbal shell you're looking at is designed to support being modified in semi-straightforward fashion. Let's look at a real example -- the editor doesn't line wrap by default, which is sometimes inconvenient:

```
edit('/sites/gimbal.melanic.org/live/src/README.md')
```

(TODO - screenshot)

See how inconvenient that is? It's okay though. (Note - you must have a guest account for this to work; make sure you have done `login{guest:1,g:1})` from above. Be aware that guest accounts are ephemeral and regularly deleted.)

```
cd()
rm('lib', {r:1, f:1})
cp('/sites/gimbal.melanic.org/live/lib', '.', {r:1})
edit('lib/codemirror/gwm-widget.js')
```

(TODO - screenshot)

So now we've copied the Gimbal client app to our home directory, and we're editing the editor code (or, the wrapper around Codemirror that provides a Gimbal editing widget).

We find the place where we can add the extension that adds line wrapping:

(TODO - screenshot)

We activate the extension:

(TODO - screeenshot)

Save the file (Ctrl-S or else the green save icon on the editor's titlebar) and then:

```
cd()
m = await gsh.importLib('lib/codemirror/gwm-widget.js')
gwm.openWidget(m, { file: '/sites/gimbal.melanic.org/live/src/README.md' })
```

And, bingo bango! You should see an editor instance with line wrapping fixed:

(TODO - screenshot)

You can also, if you want to, persist this change into a custom entry in the command strip:

```
cd()
home = await pwd().toText()
mkdir('local/gimbal',{p:1})
cd('local/gimbal')
cp('/sites/gimbal.melanic.org/live/lib/gimbal/profile.jsonl','.')
echo(`window.myEditWidget = await gsh.importLib('${home}/lib/codemirror/gwm-widget.js')`, {j:1}).append('profile.jsonl')
echo(`window.myEdit = (file) => { gwm.openWidget(window.myEditWidget, { file, icon: 'edit', iconColor: 'purple' }) }`, {j:1}).append('profile.jsonl')
```

And, if you reload the page, you should be then able to write `window.myEdit({path})` and that'll open {path} for you in the modified editor. Try it! Does it work?

(TODO - screenshot)

It works on my machine. That modified editor should be persistent (at least for as long as your guest login lasts). And of course, that's not limited to just the editor -- you have a full copy of the Gimbal code in `{home}/lib/`, so you can modify anything in there. You could make a customized shell that runs from there, and it'll pick up all the code from its local `lib/` and all tools launched from it will carry the same modifications.

### Cloning a Site

So that is simple to do (relatively speaking), but leaves your system in a half-and-half state wherein you'll be loading widgets from one cloned Gimbal install into an environment hosted by the ancestor of the clone. That is fine, but also, it'll be useful to have a whole system you can edit without having that chicanery involved. That's actually simpler; it involves some vhost and permissions stuff is the only reason we started with modifying one widget. Basically, you can at any time make a full clone of an app (in this case, the entire Gimbal operating environment) which you can then modify however you'd like.

If you're interested, this is how:

#### Make your local clone

We make a copy of the `gimbal.melanic.org` site onto a new vhost. So, by design this is something that any member of the site can do. Like tumblr. We just need to make a new directory in /sites corresponding to that host. Read the permissions section in REFERENCE.md for more, but the quick version of what you need to understand about how the permissions for this go is:

* Every grant of permissions is described in terms of *both* the user being granted access and the origin (i.e. the vhost that user is on, i.e. the code the user is running.) 
* Permissions are additive as you go down the file tree from the root. You will not have any access to `/sites` *except* for the ability to create a directory in it under a name that doesn't already exist.
* The directory in `/sites/` *must* be created with its permissions already in-place. Because everyone's permissions in `/sites/` are so restricted, you cannot make a directory there and start mucking around in it. You must make the directory, set up the permissions to grant yourself owner access, and then move it into place fully formed.

So with that in mind, the first step is to make a clone of `gimbal.melanic.org` in our home directory:

```
cd()
me = await whoami().toJS()
mkdir(`gimbal.${me}.melanic.org`)
cp('/sites/gimbal.melanic.org/live',`gimbal.${me}.melanic.org/`,{r:1})
```

(There's nothing special about the `anon1895.melanic.org` hostname space or whatever; we're just doing that to keep things organized. You could write literally anything there as the name of the new vhost, as long as it doesn't collide with an existing one. You cannot "claim," in other words, the whole namespace under your specific username, which would yes be a nice thing to be able to do.)

#### Set up permissions

Now the the directory is in your home directory, you need to grant access to yourself to it. We're going to grant access to the new site from both the normal `gimbal.melanic.org` origin, and also the Gimbal shell which will be running at `gimbal.anon1895.melanic.org`. We're planning to be able to use both, which means we want to be able to edit the new site when operating either, in other words.

```
facl(`./gimbal.${me}.melanic.org/`,{u:`${me}`,o:'https://gimbal.melanic.org/'},{p:'owner'})
facl(`./gimbal.${me}.melanic.org/`,{u:`${me}`,o:`https://gimbal.${me}.melanic.org/`},{p:'owner'})
```

And, we want to grant access also to our home directory from the new customized shell we're making. Random apps do *not* have access to the home directory even if they have our auth token.

```
facl('.',{u:me,o:'https://gimbal.${me}.melanic.org/'},{p:'owner'})
```

It is worth looking over these `facl()` calls carefully if you want to understand how permissions here work. It's different from Unix. Also, note that `{o:'https://gimbal.melanic.org/'}` for the origin is usually shortened to just `{o:'gimbal'}`, but we spell them out in full here just for clarity.

So at the end of that, we've got permissions set up so that we can edit the vhost as we need. Note that our new `gimbal.anon1895.melanic.org/live` directory, where the actual web root lives, already has world read permissions, because it inherited them when we cloned `gimbal.melanic.org`. Without that, serving our web site wouldn't work for anyone whose browser wasn't carrying our auth tokens.

You can query for the world-readable permissions that we're depending on, for the world to be able to read both of these, if you type:

```
facl('/sites/gimbal.melanic.org/live')
facl(`gimbal.${me}.melanic.org/live`)
```

#### Make it live

In any case, once you've set up permissions, you can make the thing live.

```
cp(`gimbal.${me}.melanic.org`,'/sites/`,{r:1})
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
login({g:1})
test()
```

Frontend tests will take a while.

### Web serving

Once tests pass, you can start putting up content. `upload().to(filename)` and
`unzip(filename)` are useful. The copy-on-write version workflow looks like this:

```
cd('/sites/{hostname}')
mkdir('dev/v1', {p:1})
echo('version 1').to('dev/v1/index.html')
ln('dev/v1', 'live', {ff:1})
```

(The `ff` option forcibly overwrites `live` with a copy of `dev/v1`, instead of
the normal Unix behavior of creating a new file inside an existing `live/`
directory.)

To start a v2:

```
ln('dev/v1', 'dev/v2', {ff:1})
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
