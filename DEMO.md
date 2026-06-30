What follows is a quick walkthrough of what the system can do right now.

## Examples / Demo

Gimbal is easy to try without installing anything. Go to [gimbal.melanic.org](https://gimbal.melanic.org/). You may be there already.

Specifically the examples we'll show are:

1. How as an end-user to modify the code of the app you're interacting with
2. How as an end-user to clone an app into a new vhost that you're fully in charge of

(TODO - Examples don't always work)

First, open up Gimbal if you're not already there, and from the terminal widget (the `>` prompt), start a guest session and try some basic operations:

```
gimbal.login({guest:1, g:1})   /* start a guest session (persists across tabs) */
gimbal.whoami()                /* check who you are                            */

home = await gimbal.home()
home.path('hello.txt').write('hello')
home.path('hello.txt').read()
home.ls()
```

Most of what you're doing is grabbing paths within a vaguely Unix-like filesystem and calling methods on them. See [REFERENCE.md](REFERENCE.md) for more about how the whole command system works, although hopefully the basic gist is pretty apparent.

Anyway, real examples:

(TODO - check examples and make sure they actually work + polish away bad edges)

### 1. Modifying Application Code

You don't have to be an administrator to change the application code. Here's a real example.

Gimbal's editor doesn't do line wrapping:

(TODO - screenshot)

That's not convenient. I haven't fixed that in the main codebase, because it makes a good example of how end-users can patch the app code they're running.

First, get a reference to the site's webroot. Then, you can browse the files of the webroot, and double-click `src/README.md` which will show the nature of the line wrapping problem. Check it out, and then if you are comfortable finding your way back to this document, we will illustrate how to fix the code so that line-wrapping in the editor will work properly.

```
site = await gimbal.site()
site.launch('files')           /* open a file browser of the webroot  */
                               /* find and double-click src/README.md */
```

The fix is super simple -- it is one line. In order to be able to make the fix, you must make a clone of the Gimbal app code in your home directory, since you obviously aren't allowed to change the main site's editor's code. First, open up the code to the relevant part of the editor (wrapper around Codemirror):

```
site.path('lib').ln(await home.path('my_gimbal'))
home.path('my_gimbal/lib/codemirror/gwm-widget.js').launch('edit')
```

Next, make this one-line fix to add to Codemirror the line-wrapping extension:

(TODO — screenshot showing the one-line fix)

Hit ctrl-S to save, then open the file using your patched editor to confirm it works:

```
site.path('src/README.md').launch(await home.path('my_editor/gwm-widget.js'))
```

(TODO — screenshot)

Bingo bango. You have a changed version of the site's editor, only for yourself. This will persist into future logins; as long as you are logged in as you, you can use the modified version of the widget for editing by repeating that same command.

To see one way to make it easier to access (so that, instead of having to paste a special command to edit a file with the modified version of the site's editor, your changed version simply becomes "the editor" for you), see the next section.


### 2. Cloning and Modifying a Site

The widget-patching approach above is useful for small changes, but it leaves you in a mixed state — your code layered on top of someone else's host. If you want a fully independent copy of an app that you can modify freely, you can clone the entire vhost.

#### Make a local clone

We'll copy the `gimbal.melanic.org` site onto a new vhost. Any member of the site can do this. The key thing to know about permissions first:

* Every permission grant specifies both a *user* and an *origin* (the vhost the user is coming from).
* Permissions are additive as you go down the tree — there's no way to revoke a permission on a subdirectory once a parent has it.
* A new directory in `/sites` must be created with its permissions already in place, then moved there fully formed (because your permissions in `/sites` itself are minimal).

With that in mind:

```
home = await gimbal.home()
site = await gimbal.site()

// Make a staging area in your home directory and clone the site into it
vhost = home.path('gimbal.YOURNAME.melanic.org')
await vhost.mkdir()
vhost.path('live').ln(await site.path('live'), {ff:1})
```

#### Set up permissions

Grant yourself owner access to the new vhost from both the original Gimbal origin and the new one you're creating:

```
gimbal.facl(vhost.abs(), {u:'YOURNAME', o:'gimbal'}, {p:'owner'})
gimbal.facl(vhost.abs(), {u:'YOURNAME', o:'gimbal.YOURNAME'}, {p:'owner'})
```

Also grant your new shell access to your home directory:

```
gimbal.facl(home.abs(), {u:'YOURNAME', o:'gimbal.YOURNAME'}, {p:'owner'})
```

#### Make it live

```
vhost.ln('/sites/gimbal.YOURNAME.melanic.org', {ff:1})
```

Then load `https://gimbal.YOURNAME.melanic.org/` and you should see a complete Gimbal environment — yours, fully independent.

(TODO — screenshot)

From here you can change anything, and what you end up with is a real independent site, not a tweak layered on top of someone else's.

---

## Further Reading

* [INSTALL.md](INSTALL.md) — how to run your own backend
* [REFERENCE.md](REFERENCE.md) — full technical reference: permissions, API, filesystem layout, storage format

---