# Gimbal browser-side code

This is where all the frontend code lives.

## Shell

The terminal uses the `>` prompt. All commands are methods on `gsh`. Examples:

```
gsh.login({guest:1})               // log in as guest
gsh.whoami()                       // see who you are
gsh.ls('/')                        // list root directory
gsh.read('/path/to/file.txt')      // read a file
gsh.path('/path/to/new.txt').w('hi')  // write a file
gsh.home()                         // your home directory path
gsh.home().ls()                    // list your home
gsh.test()                         // run self-tests
gsh.help('ls')                     // help for a command
```

For filesystem operations, you can pass paths as plain strings to most commands:

```
gsh.ls('/home')
gsh.mkdir('/home/you/newdir')
gsh.rm('/home/you/oldfile.txt')
gsh.read('/home/you/file.txt')
```

Or use `gsh.path()` to create a path object for method chaining:

```
gsh.path('/home/you').ls()            // list a directory
gsh.path('/home/you/file.txt').w('x') // write to a file
gsh.path('/src').cp('/dest')             // copy between paths
```

All commands in lib/*/main.js:
  Filesystem: ls, cp, mv, rm, mkdir, rmdir, ln, diff, read, write, append, unzip, path
  Auth/Info: login, logout, whoami, help, test, home
  Utilities: upload, download, message
  Widgets: gterm, edit, codemirror, files, iframe, markdown, inbox
