# grits - Decentralized CDN for powerful community-driven web hosting

Grits is load-sharing software, designed to allow a community-supported web site to operate based on direct contribution of hosting resources by the members of the site. A site using grits proxies should be able to shift a lot of the cost of operating the site onto the members of the community by having them run pretty simple software, while still operating in a fast and secure manner.

The motivation is that for at least 20 years, people have been talking about switching to a more peer-to-peer vision of the internet, but the actual success that's been demonstrated has always been limited in scope compared to the non-peer-to-peer options. Bittorrent is great, ActivityPub is great, but Wikipedia still runs on expensive centrally-served hosting. My vision would be that it becomes realistic to run a busy Mastadon node, or a site like Wikipedia, and have a substantial amount of the hosting being done by the users. As I understand it, it's already becoming expensive to run a Mastodon node with any level of activity, and I would like for this to be an immediate available solution to that.

So how it works is that the site's client-side code includes the ability to fetch static content from a swarm of user-operated proxies which provide a content-addressable store, verify that the hash of any blocks that come back, and provide it to the browser as if it had come from the central server. Thus the central server has a lot less load. And thus, whoever operates the central instance has lower hosting bills, their organization has less pressure to create a profit or solicit donations, and the internet gets better.

This first cut, being able to operate as a sort of community CDN, is actually a precursor to what I'd *actually* like to do, which is to enable a type of web app where most of the app is defined purely on the client-side, and the server is primarily just responsible for CRUD semantics and revision history and permissions on the shared store. I think that'll carry a ton of benefit in terms of security, performance (potentially, depending on how it's done), and user-configurability / empowerment. But, I think it's important to start with one piece that's clearly useful now and see how it works and how difficult it is to get it into real production.

And, even the initial cut has some significant advantages in addition to lowered load. E.g. because the whole thing is handed to the client as a [Merkel tree](https://en.wikipedia.org/wiki/Merkle_tree), you can get increased cache coherency right away -- on every page load, it gets from the central server the new root hash, so that if big directory hasn't changed at all, the client won't need to spend an RTT seeing if it's up to date, but if it has changed you'll be guaranteed to fetch the new files instead of having to shift-reload or anything like that.

## Current Status

It's still very much in progress. It doesn't work yet, just under construction. At present, I'm actually refactoring the whole thing to make use of [IPFS](https://ipfs.tech/) libraries instead of reinventing everything, but you can see some of the very limited functionality right now by doing:

```
git clone https://github.com/andrewmoise/grits.git
cd grits
cp sample.cfg grits.cfg
go run cmd/http2/main.go
```

... and you'll get a file mount on `/tmp/x` that you can fool around with. It can't do anything other than be slower than a normal file mount -- the theory is that at some point soon, it'll be possible for it to be magically synced to other servers, and served to web clients with intelligent caching. You can just put your media directory instead of /tmp/x, and that'll let you:

1. Run a node at home that keeps all the media backed up, so the central server doesn't have to even have them all stored locally, so your server storage costs aren't too high
2. Have some of your users run helper nodes that can serve up media data to your users, so your server bandwidth costs aren't too high

Like I say, it's in the middle of some refactoring right now, so it doesn't fully work. I'm currently investigating how much of IPFS can be reused without locking the software into dependencies that will make anything difficult for the end user, but the next piece involved once that's done will be to get the swarm of proxies to be able to coordinate with one another.

## Roadmap

The roadmap, more or less, is:

* First-cut of various pieces and rough testing (done)
* [Merkel tree](https://en.wikipedia.org/wiki/Merkle_tree) basic storage fundamentals (done)
* FUSE mounting (done)
* Reuse some of IPFS instead, refactor, fix and polish (WE ARE HERE - currently in progress)
* DHT and node-to-node communication (todo)
* Service worker (todo)
* Real production server tooling / testing / configurability / polish (todo)
* Performance (todo)
* Production polish and what's needed for real server operation (todo)

## Obvious questions

* Isn't this IPFS?

Yeah, kind of. I'd like to reuse, definitely at least, IPFS's block store and transport libraries. I'm not sure it makes sense to have the nodes needing to run full IPFS nodes with their 6GiB memory requirements, or to have the service worker needing to bring in the whole IPFS client library in order to just do DHT lookups, so I may want to reimplement some pieces and then have them sit on top of already-proven IPFS stuff.

Also, we're solving a substantially smaller problem than IPFS -- we're doing a small network of nodes, with a single trusted center, and so a lot of the harder problems that IPFS is solving, we don't have to pay the computational and design costs for solutions to.

* Won't this be subject to malicious nodes?

Yes, probably. We're only requesting data with a specified known hash, and verifying the hash, so it shouldn't be possible to provide poisoned data real easily, but yes you could mess up the system in other ways. I'm not envisioning everyone in the world being able to run a node in any system; it would be a semi-trusted role which if they're clearly messing up the system then you would boot them out of.

* What about performance?

I actually see performance being way better under this scheme (because you're trading a *tiny* amount of computation for a significant amount of network traffic). The only thing is you have to be a little careful that you're not getting stuck in an extended back-and-forth of round trips looking up a single piece of data -- at present, the primitive is to look up a piece of data by name and you get back all the hashes of all the directory nodes between the root and the thing you're looking for (so you can know what in your cache is fresh, and so you only need 2 RTTs to grab any given piece of data).

* What about losing data?

So this is one place where I think centralizing gets a big advantage over something like IPFS's more ambitious scope... we can afford to simply say, if you want your data to exist it's your job to pin it (and make sure you have enough nodes online with the whole thing pinned to handle any outages), if you make a big write it's your job to keep that node online until all the data has synced to the pin-maintaining nodes. And so on. Nothing comes "for free" in terms of the system maintaining data for you; you have to make sure nodes are online to keep it available.

* How do we incentivize people to contribute, and prevent free riders?

Same answer. Since we're in a relatively small "all friends here" network, we can afford to just have all the nodes blast out data to whoever requests it, and if someone's being obnoxious to the point that it creates an issue then it's dealt with administratively instead of technologically.

## Contributing

If you're interested in trying out running a node once it gets to that point, you can star the repo and I'll send an update, or you can reach me on Mastodon at mose@hachyderm.io. In the meantime feel free to check out the code (although, again, it's still super rough -- the `dev` branch is where the active refactoring is happening) and let me know what you think of the concept or the implementation.

## Enjoy!

Comments? Questions? Feedback? Let me know.