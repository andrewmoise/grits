# Greetings and Salutations

This is a little demo instance for my project called Gimbal.

So basically, it is a little imitation of a Unix shell, that runs in the browser. You've got a file browser and terminal over to your right. There is a specific reason why all this exists and is allegedly useful, the core of which is this:

**The modern web sure isn't great.**

One among a few different reasons is that when you're on the web, the code that is assembling your page, and the data underlying it, is at best visible "under glass" and usually not at all. What data is this thing logging about you? What algorithm is assembling your feed? No idea, and too much of the time it is being done in a way that's at least a little bit malicious. It is your computer, it is your browser, but you still can't right-click and save the image / stop the video ad from playing / whatever. Why? Because they don't want you to.

It is the central paradigm of the web. Honestly, a little bit of the badness of the whole setup impacts you even when visiting a civilized site on Mastodon or whatever. You still cannot change the code unless you feel like self-hosting. If Mastodon's search sucks, there's not much realistic that you are going to do about it.

So Gimbal is a little project for making web apps in which the app code is open source *from the perspective of the user at the browser*. You can see any data that your piece of the app is allowed to see. You can modify whatever site code you want to, without violating site security or impacting other users by changing their experience.

Honestly that is just the way it should be. Remember Usenet? Remember early Linux? I have no idea when this idea came in that the normal experience of the computer involves some faraway chucklehead determining for me what I am and am not allowed to do. But however it happened, that whole paradigm for the web was always a little bit of a problem, and now in the modern day it has gone absolutely way too far. This project is just an attempt at offering to the web a more civilized experience which *used* to be the normal way of a lot of people's computing environments.

## What is this?

In specific terms: This is a web framework designed for in-browser admin and development. The frontend you are seeing now is Gimbal, and it runs on a read/writable storage backend called Grits. They operate together to provide a framework within which you the web user (if you are tech savvy) can exist as a full citizen, not a helpless consumer of whatever experience the site owners have curated for you. The fact that it kind of looks like Javascript trying to pretend to be Unix is not the important part; that's just to give a familiar interface around the whole thing. 

The whole project is not real complete yet. It's a neat idea, according to me, but the server still needs a lot of work. Also, this is just my dev server. It might or might not be running at any given time, and I plan at least one big metadata format change which will reset everything.

That being said, you can try some simple interactions here now in the terminal window over on your right. Just a note -- these guest accounts are basically session tokens. They and the files created in them are not persistent for any long length of time; it's just to get a feel for the nature of the environment.

Anyway, to try, at the `live $` prompt:

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


You can also do things like `files()` and `edit()` and `gterm()`, if you're comfortable finding your way back to this document when the windows rearrange.

## How to Learn More

There's a more detailed README with some further examples, including showing actually editing the code to the app and the resulting changes showing up for your user. Type:

```
markdown('/sites/gimbal.melanic.org/live/src/README.md')
```

You can also take a look at the beginnings of a detailed reference about how the whole thing works:

```
markdown('/sites/gimbal.melanic.org/live/src/REFERENCE.md')
```

There's also a Matrix room at `#gimbal:matrix.org`.

You can also reach me by email at [moise@melanic.org](mailto:moise@melanic.org)

Cheers and have fun.

-Andy
