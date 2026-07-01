# Demo

What follows is a quick walkthrough of what the system can do right now.

Gimbal is easy to try without installing anything. Go to [gimbal.melanic.org](https://gimbal.melanic.org/). You may be there already.

Specifically the examples we'll show are:

1. How as an end-user to modify the code of the app you're interacting with
2. How as an end-user to clone an app into a new vhost that you're fully in charge of

First, open up Gimbal if you're not already there. To see the basics of filesystem operations, you can try some simple commands at the terminal prompt (the blue `>`):

```
gimbal.login({guest:1, g:1})   /* start a guest session (persists across tabs) */
gimbal.whoami()                /* check who you are                            */

home = await gimbal.home()
home.p('hello.txt').write('hello')
home.p('hello.txt').read()
home.ls()
home.p('hello.txt').cp(await home.p('again.txt'))
home.ls()
```

Most of what you're doing is defining path objects within a vaguely Unix-like filesystem and calling methods on them. See [REFERENCE.md](REFERENCE.md) for more about how the whole command system works, although hopefully the basic gist is clear.

* `.p()` means `.path()` (which also works if you want that)
* `home.p('foo').p('bar/baz.txt')` gives you conceptually `$HOME/foo/bar/baz.txt`
* You can type `gimbal.root()`, `gimbal.home()`, and `gimbal.site()` to look at some of the basic paths involved, to get some orientation about where things are in the centralized file space.

Note the use of `await` in places. Basically every function is async here, since it's all potentially doing network things to talk with the central Grits filesystem stored on the server. We hide some of the messiness of that by using this chain-of-promises syntax, and by having the JS terminal automatically `await` if you type a command that's a promise (which Gimbal-related things generally will be). But! If you are using the result of an expression, within your command, you must `await` explicitly, so our evaluator doesn't have to become too tortured and can just deal with raw Javascript. You can see that `await` in a couple of places above.

Anyway, real examples:

## 1. Modifying Application Code

You don't have to be an administrator to write custom application code. Here's a real example.

Gimbal's editor doesn't do line wrapping:

![Screenshot showing busted line wrapping](doc/images/custom-0.png)

That's not convenient. I haven't fixed that in the main codebase, because it makes a good example of how end-users can patch the app code they're running.

You can try this yourself. Open up the file browser (the folder icon on your left), open up "src", and double-click "DEMO.md" and you'll see all that un-line-wrapped glory. Then, find your way back here, and I'll show fixing it.

The first part of fixing the editor is to make a little clone of the Gimbal code in your own file space:

```
gimbal.site().p('lib').cp(await gimbal.home().p('lib'))
```

Second step is to edit the code of the editor, which is a Gimbal widget wrapper around Codemirror. The change we need to make is this:

![Screenshot showing how to fix line wrapping](doc/images/custom-1.png)

And, to get to where we can make that change, we do this:

```
gimbal.home().p('lib/codemirror/gwm-widget.js').launch('edit')
```

(In practice, if you noticed something broken in the framework, you would do `gimbal.home().launch('files')` and poke around in `lib/` for a while until you figured out what was responsible and what to do about it. You are free to do that to find `/lib/codemirror/gwm-widget.js` instead of directly opening the offending source, if you would like a more thorough demo.)

(Also, note, you obviously can't do this with the actual site code in `gimbal.site()`; it's only because it is in `gimbal.home()` that it works.)

Anyway, once the edit is made, the fourth step is to save the file (Ctrl-S). Fifth step is to launch the modified widget into your existing environment:

```
gimbal.site().p('src/DEMO.md').launch(await gimbal.home().p('lib/codemirror/gwm-widget.js'))
```

Did it work? It worked for me:

![Screenshot showing fixed line wrapping](doc/images/custom-2.png)

Note that if you change the source *again*, you must reload the tab for the changes to take effect. We don't try to monkey with Javascript's `import()` semantics, so once something's imported you'll have to reload the tab. It only worked the first time because we had never imported `home()/lib/codemirror/gwm-widget.js` until after we fixed it.

Changing or examining the code of a running web app really doesn't take long, once you are familiar with the lay of the land. We can't modify the "core" functionality of the site, because we're obviously not allowed to modify actual code running `gimbal.melanic.org` (although, see the next section!), but we can run custom widgets from `home()/lib` which we define, which lets us fix bugs in the widgets and then use the fixed versions instead.

If you're up for a little more in-depth example, you're welcome to proceed to the next section, where we will deploy a modified clone of the whole gimbal.melanic.org, which lets us fix things anywhere without needing custom launch commands to run the editor or anything like that.

## 2. Cloning and Modifying a Site

The widget-patching approach above is useful for small changes, but if you want a fully independent copy of the app as a whole that you can modify freely, you can clone the entire vhost. So in this example we'll do exactly that, under `gimbal.{YOUR USERNAME}.melanic.org`.

### Make a local clone


```
home = await gimbal.home()
site = await gimbal.site()
me = await gimbal.whoami()

/* Make a staging area in your home directory and clone the site into it */
vhost = home.p(`gimbal.${me}.melanic.org`)
vhost.mkdir()
site.cp(await vhost.p('live'))
```

(Note a subtle point -- a path doesn't have to exist for us to operate on it. We create the object for `$HOME/gimbal.{me}.melanic.org` before we `mkdir()` it. Paths exist independent and separate from files, and they also don't move along with the files when the files move.)

### Set up permissions

So, allowing random vhosts to be defined, which then operate within the whole space of data with the other vhosts, obviously carries some scary implications about what those new apps might do to everyone's data.

Our solution to this is to define access in terms of *both* the user and the origin involved. Your auth token is good across all vhosts, and they all access a single file space, but they don't all have the same access. None of them have *any* access until it's explicitly granted. You can read about the details in [REFERENCE.md](REFERENCE.md), but the short implication is that we'll need to explicitly grant access for this new vhost before it can accomplish anything.

First, in order for your new shell to be useful, you'll need to access your normal files from it. (Most apps, you will *not* want them to do this; some random music player on `music.melanic.org` needs to be safe to make use of without thus giving it access to your whole home directory.)

```
home.allow({u:me, o:`gimbal.${me}`, p:'owner'})
```

Next, in order to keep editing the new vhost you're making, once it's been moved into place in `/sites` and become a live vhost, you need to grant access to it to yourself. It's temporarily under an umbrella of `/home/{you}` but that will stop being true and the grant for your home directory will stop working, unless before it leaves `home()` we do this:

```
vhost.allow({u:me, o:'gimbal', p:'owner'})
vhost.allow({u:me, o:`gimbal.${me}`, p:'owner'})
```

### Make it live

Now, having assured that access is in place, move the vhost into place to make it live:

```
vhost.mv(await gimbal.p(`/sites/gimbal.${me}.melanic.org`))
```

Once it's there, it's live. Load `https://gimbal.{your name}.melanic.org`. The first load WILL fail -- the server needs your attempt in order to start setting up certificates for you. Wait about 5-10 seconds. This should work more smoothly (TODO), but for now, you have to do the request to kick off the certificate process, and then once the certificate's set up, the site will start working.

Once you've waited, load it again:

![Fully customized site](doc/images/vhost-0.png)

Did it work?

If it worked, you're in! Congratulations. You now have a whole clone of `gimbal.melanic.org` which you can fully control, which means you can hack the code however you like without weird little launch() workarounds. You're still you, and all your files are still there (in the store which is shared with `gimbal.melanic.org`):

```
gimbal.whoami()
gimbal.ls()
```

To make the line-wrapping fix in more permanent fashion this time, you can now just directly edit code in `/lib/`:

```
gimbal.site().p('lib/codemirror/gwm-widget.js').launch('edit')
```

Remake the editor fix, Ctrl-S, and reload the tab.

![Fixed editor in new vhost](doc/images/vhost-1.png)

Now your editor's fixed forever. And, you can wander around in `src/` changing other stuff. If you brick the thing, you can restore from `/sites/gimbal.melanic.org/live` to get back to a working state. And so on.

Neat stuff, right? I think it's neat.

## Roadmap

You can check out [TODO.md](TODO.md) to see my thoughts on what's being worked on next. There is a *ton* of stuff that I would like to make happen. Right now it is all in sort of embryonic state. 

If you made it this far, also, [drop me a line](mailto:moise@melanic.org) and tell me what you think about it.

## Further Reading

* [INSTALL.md](INSTALL.md) — how to run your own backend
* [REFERENCE.md](REFERENCE.md) — full technical reference: permissions, API, filesystem layout, storage format