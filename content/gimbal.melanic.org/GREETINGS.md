# Greetings and Salutations

This is a little demo instance for my project called Gimbal.

So basically, it is a little Javascript shell, that runs in the browser. You've got a file browser and terminal over to your right. There is a specific reason why all this exists and is allegedly useful, the core of which is this:

**The modern web sure isn't great.**

One among a few different reasons is that when you're on the web, the code that is assembling your page, and the data underlying it, is at best visible "under glass" and usually not at all. What data is this thing logging about you? What algorithm is assembling your feed? No idea, and too much of the time it is being done in a way that's at least a little bit malicious. It is your computer, it is your browser, but you still can't right-click and save the image / stop the video ad from playing / whatever. Why? Because they don't want you to.

So Gimbal is a little project for making web apps in which the app code is open source *from the perspective of the user at the browser*. You can see any data that your piece of the app is allowed to see. You can modify whatever site code you want to, without violating site security or impacting other users by changing their experience.

## What is this?

This is a web framework designed for in-browser admin and development. The frontend you are seeing now is Gimbal, and it runs on a read/writable storage backend called Grits.

That being said, you can try some simple interactions in the terminal window over on your right. (Guest accounts are ephemeral — they don't persist for long.)

At the `>` prompt, try:

```
gimbal.login({guest:1})                // Start a guest session
gimbal.whoami()                        // See who you are
gimbal.home()                          // Your home directory path
gimbal.home().w('hello')               // Create a file in your home
gimbal.home().ls()                     // List files in your home
gimbal.p('/').ls()                     // List the root directory
```

You can also try `gimbal.gterm()`, `gimbal.files()`, or `gimbal.edit('/path')` to open new windows.

## How to Learn More

Read the full README:

```
gimbal.p('/sites/gimbal.melanic.org/live/src/README.md').read()
```

Or the reference document:

```
gimbal.p('/sites/gimbal.melanic.org/live/src/REFERENCE.md').read()
```

There's also a Matrix room at `#gimbal:matrix.org`. Cheers and have fun.

-Andy
