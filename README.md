# A framework for web-native development

This is a little framework for development of self-hosted and/or community driven web applications.

It's still a work in progress. **This code is not production ready or close to it.** Some things work in useful form, but also it's still heavily in flux. This is mostly just a demo of the intended direction of the system, for people to mess with and give feedback on.

Some of the things this framework is designed to make easy are:

* For users to examine or delete their data on a web app without needing to go strictly through the web app's interface
* For ordinary users to modify a web app they're using for themselves, if the existing functioning doesn't suit their wants (or to clone/fork it completely)
* For admins to run a lightweight backend which can host many arbitrary apps without needing any setup of the backend specific to each app
* For admins and their friends to semi-easily add capacity (storage or networking) to the server (*)

The system that accomplishes this is two cooperating pieces:

* **Grits** is the backend. It is, more or less, a networked filesystem oriented around cached and access-controlled content.
* **Gimbal** is one frontend web app which runs atop a Grits server. It's a demo of the system, and an allegedly useful environment for doing administration and development of other apps which can exist on the system.

If you want to set up your own backend instance, see [INSTALL.md](INSTALL.md). For a detailed reference, see [REFERENCE.md](REFERENCE.md). What follows is a quick walkthrough of what the system can do right now.

## Examples / Demo

Gimbal is easy to try without installing anything. Go to [gimbal.melanic.org](https://gimbal.melanic.org/). You may be there already.

Specifically the examples we'll show are:

1. How as an end-user to modify the code of the app you're interacting with
2. How as an end-user to search through data the app is storing
3. How as an end-user to clone an app into a new vhost that you're fully in charge of

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

### 1. Browsing Your App Data

One of the goals of this system is that you can always get at the data an app is storing for you, without going through the app's own interface. Since it's just files in the Merkle tree, you can read, search, or export it directly from the shell.

(TODO — the examples here depend on apps that don't fully exist yet. But here's what that would look like for a few plausible cases:)

**A simple inbox or message store:**

```
// List everything in a hypothetical inbox app's data directory
inbox = home.path('local/inbox')
inbox.ls()

// Read a specific message
inbox.path('2024-06-01-from-alice.json').read()

// Search across all messages for a string (basic JS text search)
files = await inbox.ls()
for (const f of files) {
  const text = await inbox.path(f).read()
  if (text.includes('project deadline')) console.log(f)
}
```

**A notes or journal app:**

```
notes = home.path('local/notes')

// Find all notes mentioning a topic
const matches = (await notes.ls())
  .filter(async f => (await notes.path(f).read()).includes('gardening'))
```

**A shared community dataset in `/var`:**

```
// If an app stores shared data in /var, you can query across all users' contributions
entries = await gimbal.path('/var/my-app').ls()
for (const user of entries) {
  const data = JSON.parse(await gimbal.path(`/var/my-app/${user}/data.json`).read())
  if (data.tags?.includes('2024')) console.log(user, data.title)
}
```

The point is: because everything is files with a consistent API, you can write arbitrary queries against your own data without needing the original app to support it. There's no particular query language baked into the system right now — it's just JS — but that's sufficient for a lot of real use cases, and a more structured query layer is on the roadmap.

---

### 2. Modifying Application Code

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


### 3. Cloning and Modifying a Site

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

Live instance: [https://gimbal.melanic.org/](https://gimbal.melanic.org/)

Matrix: `#gimbal:matrix.org`

Comments, questions, feedback? [Let me know.](mailto:moise@melanic.org)
