# Greetings and Salutations

This is a little demo instance for my project called Gimbal.

## Why is this?

The modern web sure isn't great.

One among a few different reasons is that when you're on the web, the code that is assembling your page, and the data underlying it, is at best visible "under glass" and usually not visible at all. Most of the time it's just a secret. What data is this thing logging about you? What algorithm is assembling your feed? No idea, and way too much of the time, it's being done in a way that's at least a little bit malicious. It is your computer, it is your browser, but you still can't right-click and save the image / stop the video ad from playing / whatever. Why? Because they don't want you to.

That's not great. Honestly, a *little* bit of the badness of the whole paradigm leaks in even when visiting a civilized site on Mastodon or whatever. You still cannot change the code unless you feel like doing self-hosting. If Mastodon's search sucks, there's not much realistic that you are going to do about it.

So Gimbal is a little project under which the code that runs the site is open source *from the perspective of the user at the browser*. You can see any data that your piece of the app is allowed to see. You can modify whatever app code you want to, without impacting other users by changing their experience of the same app.

Honestly that is just the way it should be. Remember Usenet? Remember early Linux? I have no idea when this idea came in that the proper mechanism to experience the computer involves some faraway chucklehead determining for me what I am and am not allowed to do. But however it happened, that whole paradigm was always at least a little bit wrong, and it has now gone absolutely way too far. This project is just my little attempt at wresting the control a little bit back in the right direction.

## What is this?

So in specific: This attempt at a solution to all of that is a web framework designed for in-browser admin and development. The frontend you are seeing now is called Gimbal, and it runs on a read/writable storage backend called Grits. They operate together to serve apps which provide a civilized experience, where you the web user (if you are tech savvy) can exist as a full citizen, not a helpless consumer of the preset experience the site owners have curated for you.

You can also go to the terminal and try some simple interactions. Try the below, and then if you are feeling like getting your hands dirty, you can try the "fixing the editor's line wrapping" example from the README.

If you wind up deciding to work through the example, you must be logged in for them to work. If you see `whoami()` fail, you're not logged in; do `login({guest:1,g:1})` to create a guest user for yourself, and then `whoami()` and `cd()` to go to your home directory to do the rest. Also, you can't use `custom.melanic.org` as your vhost or your `facl` origin. Pick a different vhost instead, and use that for both the `sites/` directory and the `o:` argument to `facl()`.

Anyway. Simple shell commands you can try, in the terminal window to your right, at the `live $` prompt:

```
pwd()
ls()
files('src')
markdown('src/README.md')
```

## How is this?

If you want to learn the nitty-gritty implementation, see README.md and REFERENCE.md. It's not real complete yet. It's a neat idea, according to me, but the server still needs a lot of work. Don't let the shiny colors fool you that it's polished yet. Also, this is just my dev server. It might or might not be running at any given time, and I plan at least one big metadata format change which will reset everything.

The source is on github for now, although I plan to fully self-host it once this is a stable place to self-host. For now, try out the self-hosting; you can look over the source over on the right side, or you can check it out yourself via `git clone https://gimbal.melanic.org/src/.git gimbal`. That way is new; if it doesn't work, let me know.

If you want a real account or otherwise have any feedback, or want to play around with it more, [let me know](mailto:moise@melanic.org) and I'll set you up or address it as best I can. There should also be a Matrix room coming soon if you would like to have some chat about it.

Cheers! And have fun.

-Andy