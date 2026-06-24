# A framework for web-native development

This project is Gimbal, a little web framework which lets you interact with a web site or application much more directly and flexibly than you usually can. As an end-user here, you can:

* Directly examine the site's underlying data(*) and code
* Persistently edit the UI of the web site you're using, for you, without impacting other people's experience
* Interact and script interactions with the site in ways not programmed in advance by the authors/operators

(*) subject to user permissions

As an admin, you can:

* Easily test, deploy or roll back versions of the site
* Do first-class development and devops things from your browser

Note: This is all still a work in progress. It's a hobby project. It works on my machine. **It might eat your data at any time.** The permissions might fail. The service worker doesn't update properly. **Don't use this in production.** Basically, some things work and it's fun for tinkering, but it's pretty far from a working version 1.



## Examples

### General Interactions

Here's what it looks like:

![Screenshot of normal environment](doc/images/intro-0.png)

You can see a terminal, a files browser, and an editor. Pretty straightforward.

Terminal commands are interpreted more or less as javascript syntax. You can do javascript things:

![Screenshot showing basic JS commands](doc/images/intro-1.png)

But you can also run Unix-like commands and interact with the little filesystem:

![Screenshot showing Unix-like commands](doc/images/intro-2.png)

The files you're editing are in the server's storage. The system is backed on a Merkle tree, which makes it natural for the browser to maintain its own local version of the store while maintaining cache coherency. It also means it's easy to make copy-on-write custom versions of big things for your own use and modification.

### Modifying an app

(TODO: This is a little more complex than it needs to be, maybe. Also this "simplified" example needs a little better screenshots.)

So, if you poked around in the file browser, you might have noticed that opening a markdown file doesn't do line wrapping:

![Screenshot lib/README.md](doc/images/custom-3.png)

Not a problem. From your terminal, you can do:

```
// 1. Let's have a test account to make changes under (skip this if you are already logged in.)
login({guest:1,g:1})

// 2. Grab the Gimbal code so we can make custom versions
cd()
rm('lib',{r:1,f:1})
cp('/sites/gimbal.melanic.org/live/lib','.',{r:1})

// 3. Fire up our custom editor widget, to test
me = await whoami().toJS()
m = await import(`/grits/v1/content/primary/home/${me}/lib/codemirror/gwm-widget.js`)
gwm.openWidget(m, {r: gsh.resolvePath('lib/README.md')})
```

In `lib/` is a README which gives some general guidance about the code you can modify here:

![Screenshot lib/README.md](doc/images/custom-3.png)

Look at that -- it's hard to read because the lines aren't wrapped. Not a problem. We open up our editor widget and make the one-line fix to add the line wrapping extension:

![Screenshot showing enabling line wrapping](doc/images/editor-0.png)

Save the document, reload the tab, and open the README again:

![Screenshot showing line wrapping](doc/images/editor-1.png)

Bingo bango. Setting up the customizable environment is *slightly* complex, but once it's set up, it's actually quicker to make changes like this example functionality change, than it would be to make a change on the backend and rebuild+restart+whatever, if you were the admin of a standard-operating web app.

Hopefully this shows the general idea behind this type of environment.


### Modifying the whole site

We can also make a copy of `gimbal.melanic.org` so we can customize this whole admin interface:

![Screenshot showing making a per-user copy of the shell environment](doc/images/custom-0.png)

The calls to `facl()` are necessary. Respectively, they are:

1. Make `custom.melanic.org` editable by our user, from our normal shell
2. Make our normal files editable by our user, from the custom shell
3. Make `custom.melanic.org` editable by our user from the custom shell

See the "permissions" section in [REFERENCE.md](REFERENCE.md) for an in-depth explanation of how and why origin access controls work and why this `facl()` is necessary.

Having made the new customized shell environment, we make it live:

![Screenshot showing deploying the new shell environment](doc/images/custom-1.png)

And, that's it. Our browser can load our customized app via the new vhost. (We load once to kick off the server fetching a new certificate for us, and then load again and see this:)

![Screenshot showing the custom environment loaded](doc/images/custom-2.png)

We're now running the same app we were before, accessing the same data (since we've given it permission to), except that it is customizable.

Now you can do away with editing things from within `/home/{username}/`, and just modify the environment directly, however you would like it to function.

## Structure

The overall system is split into two cooperating pieces:

* **Gimbal** is the frontend which provides a Unix-like shell and that "window manager" shown in the examples.
* **Grits** is the backend, the server that provides a read-writable Merkle tree with useful primitives for sharing and replicating content. More or less, it is the filesystem, and Gimbal is the desktop environment.

`grits` is the server piece, the Go code. Gimbal is the stuff that lives in `client/`, the Javascript code, which gets copied into the Merkle tree store to bootstrap the in-browser application system.

Permissions are based on both origin and user. Permissions are additive; permissions granted at one directory will also apply to all of its subdirectories. This means you may not always be able to travel to the parent of a directory you are allowed to access.

Those are the basics. If you want to know more, it is in [REFERENCE.md](REFERENCE.md), but if just want to know how to run the thing:

## Quickstart

Here's how to try. It only works on Linux right now.

### Build

First, set up prerequisites:

* Install golang >= 1.25.0
* `sudo apt install fuse3 certbot npm` or equivalent

Install and build source:

* Clone the source and `cd` to the project dir
* `make test` to do backend smoke tests
* `make deps` to fetch JS modules
* `make` to build the server

### Configure

```
cp sample-config.json config.json
hx config.json # Or whatever editor
```

You will need to make changes to the config. 

Change `%USER%` to your Unix username, and `%EMAIL%` to your email (email is only needed for certbot interactions -- the system will automatically grab HTTPS certificates for you, by default, and certbot wants your email in order to do that.)

### Run

Assuming everything checks out, you're good to start the actual service (foreground-only for now, you can use `tmux` if you like):

```
sudo bin/gritsd
```

(It'll drop privileges to whatever user you configured for it, as soon as it's opened the ports it needs. If you want to try it as non-root, just configure it on a port above 1024 and run certbot by hand to get certificates if any, and then you can run without `sudo`.)

### Initial setup

Once the server is running, the FUSE mount at `mnt/` gives you access to the file store. You'll need to add users and set up the initial filesystem skeleton.

(Note - if you shut down the server while things in the FUSE mount are in use, it'll refuse to shut down so as to not leave behind a stale mount. Just close any open files, cd out of the FUSE mount, unmount the FUSE mount, and the server shutdown should automatically continue as normal and finish.)

(Also note - the sample config already contains an automatic import of `client/` to the live directory for `gimbal.{your domain}.com`, meaning the vhost which provides the normal Gimbal shell. This means that any edits you make to `client/` on your backend source checkout will automatically get copied to the frontend's file store **(overwriting any local changes!)** on every server restart. This seems to be the easiest way to do the development for now. Any other vhosts created by users, including their own copies of /sites/gimbal.{your domain}.com, won't be impacted by this auto-import.)

You'll need to initially populate the client store. The frontend code in `client/` gets automatically imported to the appropriate vhost on every backend server start. However, we need to do a one-time import of `content/skel/` to set up some other areas of the filesystem:

```
cp -r content/skel/* content/skel/.grits mnt/
```

And then, we need to create some users:

```
bin/grits adduser glenda
bin/grits adduser {your username}
```

Once that's done, create a DNS entry for `gimbal.{your domain}.com`. (It is also recommended, for a real deployment, to just make a wildcard entry for `*.{your domain}.com` -- a lot of the power of this framework comes from being able to cheaply add new vhosts.)

### Test

Once you've done all that, you should be able to log in to see the Gimbal shell at:

`https://gimbal.{your domain}.com/`

If you see the graphical interface from the screenshots, you're in. You can run the self tests if you like:

```
login({g:1})
test()
```

Frontend tests will take a while.

### Web Serving

Assuming all the tests work, you can start populating your own content. `upload().to(filename)` and `unzip(filename)` may be useful. Bear in mind that it is trivial to maintain multiple copy-on-write versions of the site content:

```
cd('/sites/{hostname}')
mkdir('dev/v1',{p:1})
echo('version 1').to('dev/v1/index.html')
ln('dev/v1','live',{ff:1})
```

(That `ff` option requests to forcibly overwrite whatever's in `live` with a copy of `dev/v1`, without the normal Unix semantics of creating a new file within `live/` if `live/` already exists.)

If you want to work on a v2 of the site:

```
ln('dev/v1','dev/v2',{ff:1})
```

And so on. You can also deploy `v2/` to a temporary vhost's `live/` to test your changes, using real auth and the same real data as the live site.

### Permissions

The permissions system is substantially different from a normal Unix system, because of the specific needs and structure of this environment.

* Permissions are additive-only; the root directory starts out denied to everyone, and grants of access increase as you go lower down the tree
* Permissions are framed in terms of *both* the user who is trying to access something, *and* the vhost they are coming from when they attempt to access it.

In other words, permissions can be (and must be) granted in extremely surgical fashion to a specific leaf-ish directory for a specific purpose. Basically, we are explicit about the principle that if you're granting access, then both the code you've granted access to, and the human sitting at the keyboard "operating" the code, are recipients of the access. This lets us be a lot more intentional about authenticating the human to our apps and letting them all access a unified store, without a malicious app being able to access things it should not.

See REFERENCE.md for a lot more detailed explanation about how this works and how to interact with the permissions system.

### Backend Administration

As mentioned, you launch the server just with `sudo bin/gritsd`. Making it runnable via systemctl is on the TODOs.

Server configuration lives in `config.json`. See [REFERENCE.md](REFERENCE.md) for some detail about what's useful to configure within it.

When you're done, hit Ctrl-C on the backend and the server should shut down cleanly. If it hangs because it can't unmount the FUSE mount, just end the processes that are keeping the FUSE mount busy and then unmount it yourself, and the shutdown should continue from there.

The intent is that mostly, once the functionality of the system is more fleshed out, administration of the system can proceed from inside the frontend (things like checking the HTTP logs, adding users, that sort of thing.) But even in the future when that is implemented more, some bootstrapping things will still be done via backend commands. The `cmdline` module is the backend interface for live server administration.

Currently, useful commands are:

* `bin/grits ping` to test that the `cmdline` module is functioning
* `bin/grits import local/path //volume/dest/path` to import files from your Linux filesystem into Grits's file store
* `bin/grits adduser username` to add a user (you will be prompted for the password)
* `bin/grits deluser username` to delete a user

## In Conclusion

See? It's neat.

You can see it in operation here: [https://gimbal.melanic.org/](https://gimbal.melanic.org/)

You can talk to me about it on Matrix, at `#gimbal:matrix.org`

(TODO: No more Discord link but we need a link to Matrix + the self hosted source)

## Enjoy!

Comments? Questions? Feedback? Let me know.