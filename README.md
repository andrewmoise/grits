# grits - Decentralized CDN for powerful community-driven web hosting

Grits is load-sharing software, designed to allow a community-supported web site to operate based on direct contribution of hosting resources by the members of the site. A site using grits proxies should be able to shift the cost of operating the site towards the members of the community by having them run pretty simple software, while still operating in a fast and secure manner.

The motivation is that for at least 20 years, people have been talking about switching to a more peer-to-peer vision of the internet, but there are significant centralized hosting issues that still haven't gone away. Bittorrent is great, ActivityPub is great, but Wikipedia still runs on expensive centrally-served hosting. People still invest in S3 to run their Mastodon nodes. My vision would be that it becomes realistic to run a busy Mastodon node, or a site like Wikipedia, and have a substantial amount of the hosting being done by the users.

How it works is that the site's client-side code includes the ability to fetch static content from a swarm of user-operated proxies which provide a content-addressable store, verify that the hash of any blocks that come back, and provide it to the browser as if it had come from the central server. Thus the central server has a lot less load. And thus, whoever operates the central instance has lower hosting bills, their organization has less pressure to create a profit or solicit donations, and the internet gets better.

This first cut, being able to operate as a sort of community CDN, is actually a precursor to what I'd *actually* like to do, which is to enable a type of web app where most of the app is defined almost all on the client side, and the server is primarily just responsible for CRUD semantics and revision history and permissions on the shared store. I think that'll carry a ton of benefit in terms of security, potentially performance, and user-configurability / empowerment. But, I think it's important to start with one piece that's clearly useful now and see how it works and how difficult it is to get it into real production.

And, even the initial cut has some significant advantages in addition to lowered load. For one example, because the whole thing is handed to the client as a [Merkle tree](https://en.wikipedia.org/wiki/Merkle_tree), you can get complete cache coherency right away -- on every page load, it gets from the central server the new root hash, so that if a big directory hasn't changed at all, the client won't need to spend an RTT seeing if it's up to date, but if it *has* changed you'll be guaranteed to fetch the new files instead of having to shift-reload or anything like that.

## Current Status

It's still very much in progress. It doesn't work yet, just under construction. At present, I'm actually refactoring the whole thing to make use of [IPFS](https://ipfs.tech/) libraries instead of reinventing everything, but you can see some of the very limited functionality right now by doing:

```
sudo apt install golang fuse3 # (or equivalent - NOTE! Go >= 2.21 is needed)

git clone https://github.com/andrewmoise/grits.git
cd grits
go test ./...

cp sample.cfg grits.cfg
mkdir certs
sudo cp /etc/letsencrypt/live/$SERVER/privkey.pem certs/
sudo cp /etc/letsencrypt/live/$SERVER/fullchain.pem certs/

go run cmd/http2/main.go
```

Then, from a separate console:

```
echo hello > /tmp/grits-test/test
curl https://localhost:1787/test
```

You can make any type of changes to /tmp/grits-test that you want, and they'll be synced to the merkle tree, and exposed via the web root. Not much other than that works. There's also an API at /grits/v1/ that can do various operations, suitable for use from a serviceworker or another node in a swarm, but it doesn't do all that much other than that, right now.

Don't do heavy write loads (npm compiles or etc). It'll rewrite tons of copies of the merkle tree and your storage will fill up.

The theory is that at some point soon, it'll be possible for it to be magically synced to other servers, and served to web clients with intelligent caching. You can just put your media directory instead of /tmp/x, and that'll let you e.g.:

1. Run a node at home that keeps all the media backed up, so the central server doesn't have to have rarely-accessed stuff stored locally, so your server storage costs aren't too high
2. Have some of your users run helper nodes that can serve up media data to your users, so your server bandwidth costs aren't too high

There are also bits and pieces of the web API, the service worker, things like that, but at present it's in the middle of some refactoring, so it doesn't fully work.

## Roadmap

The roadmap, more or less, is:

* First-cut of various core pieces and rough testing (done)
* [Merkle tree](https://en.wikipedia.org/wiki/Merkle_tree) basic storage fundamentals (done)
* FUSE mounting (done)
* Reuse some of IPFS instead, refactor, fix and polish (WE ARE HERE - currently in progress)
* DHT and node-to-node communication (todo)
* Service worker (todo)
* Real production server tooling / testing / configurability / polish (todo)
* Performance (todo)
* Production polish and what's needed for real server operation (todo)

## Obvious questions

### Isn't this IPFS?

Yeah, kind of. I'd like to reuse, definitely at least, IPFS's block store and transport libraries. I'm not sure it makes sense to have the nodes needing to run full IPFS nodes with their 6GiB memory requirements, or to have the service worker needing to bring in the whole IPFS client library in order to just do DHT lookups, so I may want to reimplement some pieces and then have them sit on top of already-proven IPFS stuff.

Also, we're solving a substantially smaller problem than IPFS -- we're doing a small network of fairly persistent non-anonymous nodes, with a single trusted center, and so a lot of the harder problems that IPFS is solving, we don't have to pay the computational and design costs for solutions to.

### Won't this be subject to malicious nodes?

Yes, probably. We're only requesting data with a specified known hash, and verifying the hash, so it shouldn't be possible to provide poisoned data real easily, but yes you could mess up the system in other ways. I'm not envisioning everyone in the world being able to run a node in any system; it would be a semi-trusted role which if they're clearly messing up the system then you would boot them out of.

### What about performance? Dropped nodes? NAT?

So, all of these are solved problems within IPFS -- my plan is to try to be lightweight with implementation where possible, but there's always the option (particularly with NAT and maintenance of the swarm) to just back up and punt to using IPFS, and focus on the stuff that's genuinely new design ideas.

### What about privacy?

Yes. You're exposing your IP address to the world if you decide to participate in the swarm. That's potentially an issue; we'll have to be a little careful about who we expose the proxy network to, and to make sure that people are anonymous if they do participate in the proxy network, but even so it'll be something to be careful with.

### What about losing data?

We can afford to simply say, if you want your data to exist it's your job to pin it (and make sure you have enough nodes online with the whole thing pinned to handle any outages), if you make a big write it's your job to keep that node online until all the data has synced to the pin-maintaining nodes. And so on. Nothing comes "for free" in terms of the system maintaining data for you; you have to make sure nodes are online to keep it available.

### How do we incentivize people to contribute resources, and prevent free riders?

So since we're in a relatively small "all friends here" network, we can afford to just have all the nodes blast out data to whoever requests it, and if someone's being obnoxious to the point that it creates an issue then it's dealt with administratively instead of technologically.

## Contributing

If you're interested in trying out running a node once it gets to that point, you can star the repo and I'll send an update, or you can reach me on Mastodon at mose@hachyderm.io. In the meantime feel free to check out the code (although, again, it's still super rough -- the `dev` branch is where the active refactoring is happening) and let me know what you think of the concept or the implementation.

## Enjoy!

Comments? Questions? Feedback? Let me know.