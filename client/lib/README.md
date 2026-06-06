# Gimbal browser-side code

This is where all the frontend code lives.

A few different concerns are flattened together into subdirectories of this single `lib/` directory, which may or may not be a good idea.

There is also `//client/serviceworker` which contains the SW. It's only enabled if the `serviceworker` backend module is active, and unlike the rest of this, cannot be changed per-user.

## Client-side Gimbal app

`gimbal/` contains a "window manager" or full-featured graphical frontend.

Open https://{your server}/grits/v1/content/client/lib/gimbal to interact with it. There are also some JS libraries.

Conceptually, `gsh` is the command shell, and `gwm` is the window manager. Gimbal is the whole frontend package.

## Libraries

There are some main libraries here, that are generally imported directly and used from JS.

* `gimbal/` also has libraries for the window manager frontend, and contains the command shell in `gsh.js`.
* `grits/` has client-side Grits code which provides filesystem operations for content from the server.
* `style/` defines colors and styles for all UI elements.
* `vendor/` should have json-stringify-pretty-compact in it. I would like to have a better way to organize this.

## Shell commands

Any directory with a `main.js` in it will define a shell command which will then be runnable from the shell.

Many are familiar from Unix: `cat`, `cd`, `cp,` and so on.

Some are custom:

* `from(filename)` and `to(filename)` provide functionality equivalent to `<` and `>`.
* `upload()` prompts for an upload from your browser, allowing you to place it in the filesystem. Use `upload().to(filename)`. Note that if you cancel the upload, this command will hang forever; this is a browser limitation. Just close the terminal window.
* Likewise `download(url)` fetches a file from a URL. It is almost useless in practice because of CORS restrictions.
* `help()` prints some pretty brief help text.
* `test()` runs a self test on a bunch of shell commands.

## Widgets

Widgets are windows within the window environment. You can launch these either from the launcher icons in gwm, or by typing their shell commands:

* `codemirror(filename)` launches a codemirror editor.
* `edit(filename)` just launches Codemirror, but you could override it if you wanted a different editor by default.
* `files(path)` is a file browser. You can give it an argument to open a specific directory.
* `gterm()` is a terminal.
* `iframe(url)` launches a widget which displays a web page. Like `download()`, CORS makes it almost entirely useless in practice, except for testing your own cooperating applications.

## Enjoy!