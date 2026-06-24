# Greetings and Salutations

This is a little demo instance for my project called Gimbal.

## Why is this?

The modern web sure isn't great.

One among a few different reasons is that when you're on the web, the code that is assembling your page, and the data underlying it, is at best visible "under glass" and usually not at all. What data is this thing logging about you? What algorithm is assembling your feed? No idea, and too much of the time it is being done in a way that's at least a little bit malicious. It is your computer, it is your browser, but you still can't right-click and save the image / stop the video ad from playing / whatever. Why? Because they don't want you to.

That's not great. Honestly, a little bit of the badness of the whole paradigm impacts you even when visiting a civilized site on Mastodon or whatever. You still cannot change the code unless you feel like self-hosting. If Mastodon's search sucks, there's not much realistic that you are going to do about it.

So Gimbal is a little project for making web apps in which the app code is open source *from the perspective of the user at the browser*. You can see any data that your piece of the app is allowed to see. You can modify whatever site code you want to, without violating site security or impacting other users by changing their experience.

Honestly that is just the way it should be. Remember Usenet? Remember early Linux? I have no idea when this idea came in that the normal experience of the computer involves some faraway chucklehead determining for me what I am and am not allowed to do. But however it happened, that whole paradigm was always at least a little bit wrong, and it has now gone absolutely way too far. This project is just an attempt at groundwork lending to the web a more civilized experience which *used* to be the normal way for a lot of people's computing environments.

## What is this?

So in specific term: This is a web framework designed for in-browser admin and development. The frontend you are seeing now is Gimbal, and it runs on a read/writable storage backend called Grits. They operate together to provide a framework within which can be developed apps where you the web user (if you are tech savvy) can exist as a full citizen, not a helpless consumer of the preset experience the site owners have curated for you.

You can go to the terminal and try some simple interactions for yourself. Try these in the terminal window to your right, at the `live $` prompt:

```
pwd()
ls()
whoami()
login({guest:1,g:1})
whoami()
cd()
pwd()
echo('hello').to('hello.txt')
ls()
```

You can also do things like `files('/sites/gimbal.melanic.org/live')`, assuming you're comfortable finding your way back to this document when the windows move around. There's also a more complete example in the README showing modifying the code of an existing widget and then using the modified version as a normal site-user.

Note! Guest account files may be deleted after a day or so.



## How is this?

If you want to learn the nitty-gritty implementation and some reference details, you can read:

```
markdown('/sites/gimbal.melanic.org/live/src/README.md')
markdown('/sites/gimbal.melanic.org/live/src/REFERENCE.md')
```

The whole project is not real complete yet. It's a neat idea, according to me, but the server still needs a lot of work. Don't let the shiny colors fool you that it's polished yet. Also, this is just my dev server. It might or might not be running at any given time, and I plan at least one big metadata format change which will reset everything.

The source is on github for now, although I plan to fully self-host it once this is a stable place to self-host. For now, try out the self-hosting; you can look over the source over on the right side, or you can check it out yourself via `git clone https://gimbal.melanic.org/src/.git grits`. That way is new; if it doesn't work, let me know.

If you want a real account or otherwise have any feedback, or want to play around with it more, [let me know](mailto:moise@melanic.org) and I'll set you up or address it as best I can. There should also be a Matrix room coming soon if you would like to interact about it.

Cheers! And have fun.

-Andy
