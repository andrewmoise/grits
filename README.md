# grits - Decentralized CDN for powerful community-driven web hosting

Grits is load-sharing software, designed to allow a community-supported web site to operate based on direct contribution of hosting resources by the members of the site. A site using grits proxies should be able to shift the cost of operating the site towards the members of the community by having them run pretty simple software, while still operating in a fast and secure manner.

The motivation is that for at least 20 years, people have been talking about switching to a more peer-to-peer vision of the internet, but there are significant centralized hosting issues that still haven't gone away. Bittorrent is great, ActivityPub is great, but Wikipedia still runs on expensive centrally-served hosting. People still invest in S3 to run their Mastodon nodes. My vision would be that it becomes realistic to run a busy Mastodon node, or a site like Wikipedia, and have a substantial amount of the hosting being done by the users.

How it works is that the site's client-side code includes the ability to fetch static content from a swarm of user-operated proxies which provide a content-addressable store, verify that the hash of any blocks that come back, and provide it to the browser as if it had come from the central server. Thus the central server has a lot less load. And thus, whoever operates the central instance has lower hosting bills, their organization has less pressure to create a profit or solicit donations, and the internet gets better.

This first cut, being able to operate as a sort of community CDN, is actually a precursor to what I'd *actually* like to do, which is to enable a type of web app where most of the app is defined almost all on the client side, and the server is primarily just responsible for CRUD semantics and revision history and permissions on the shared store. I think that'll carry a ton of benefit in terms of security, potentially performance, and user-configurability / empowerment. But, I think it's important to start with one piece that's clearly useful now and see how it works and how difficult it is to get it into real production.

And, even the initial cut has some significant advantages in addition to lowered load. For one example, because the whole thing is handed to the client as a [Merkle tree](https://en.wikipedia.org/wiki/Merkle_tree), you can get complete cache coherency right away -- on every page load, it gets from the central server the new root hash, so that if a big directory hasn't changed at all, the client won't need to spend an RTT seeing if it's up to date, but if it *has* changed you'll be guaranteed to fetch the new files instead of having to shift-reload or anything like that.

## Current Status

It's still very much in progress. It doesn't work yet, just under construction.

General guide:

* Basics of persistent file storage and basic web serving - Done, but at a first cut level.
* FUSE - Unreliable. It will work for basic stuff, but it's not safe enough to trust your persistent storage to.
* Node-to-node communication - In progress
* Client and service worker pieces - In progress

## Quickstart

### Prerequisites

First, install Go. At present, at least version 1.22.12 is needed.

Then, do this or whatever the equivalent is on your system:

```
sudo apt install fuse3 certbot
```

### Build

Get the source. `cd` to the directory you checked out.

Check that everything is okay first.

```
go test ./...
go build -o bin/gritsd cmd/gritsd/main.go
```

### Configure

```
cp sample.cfg grits.cfg
nano grits.cfg # Or whatever editor
```

You will need to make significant changes to the config, please edit it and fill in values where requested (and delete the explanation about certbot, whether or not you are planning to run it separately).

### Port setup

The default config is designed to run as non-root, on port 8443. You will probably want to arrange either forwarding via nginx, or if you're not running nginx, you can arrange for forwarding via a helper script:

```
sudo bin/port-helper add -s 443 -d 8443
sudo bin/port-helper add -s 80 -d 8080
```

If you're doing TLS, then you'll need both, since you will need port 8080 to be forwarded for certbot. You can also simply run certbot by hand. See the sample config file for more information.

### Run

```
bin/gritsd
```

### Test

At that point, ./content will be a FUSE mount of the content store. You can test it out by putting some stuff in ./content/public, and whatever you put there should be immediately reflected in the web server. 

```
cd grits/content
mkdir public
cd public
echo hello > index.html
curl https://(your hostname)/index.html
```

You can also check out a little client self test, at:

```
https://(your hostname)/grits/v1/content/client/client-test.html
```

When you're done, hit Ctrl-C, the server should shut down cleanly. If it hangs because it can't unmount the FUSE mount, just end the processes that are keeping the FUSE mount busy and then unmount it yourself, and the shutdown should continue from there.

## More in-depth setup

So: This is still a work in progress. It's just in development. If you're psyched enough about the whole thing to want to play around with it more, here's how, but be warned that it's still in heavy development and pretty far from the easy deployment stage of things.

### Service worker

Okay, so with the simple setup, you should have a working web server. If you want to have more of (any of) the actual benefits of using a server that uses this approach, the first thing to enable is the service worker. The necessary stuff is already turned on from the server side, but you'll need to enable the right stuff in the browser in order for it to actually do something productive.

Note that I have only tried the service worker on Chrome, and I would expect it to be a little flaky in other browsers. Buyer beware.

Anyway. Navigate to:

```
https://(your hostname)/grits/v1/content/client/client-test.html
```

Click the appropriate button to request loading the service worker. You can also do this, from within whatever client-side stuff you're doing:

```
<script src="/grits-bootstrap.js" async></script>
```

Now, you should start getting in the javascript console, in addition to some log spam, stuff like:

```
[GRITS STATS] Last 10s: Requests: 3 | Content lookups: 1 (avg 139.30ms) | Blob cache hits: 2 (avg 1.90ms) | Blob cache misses: 0 | Prefetch successes: 5
GritsClient-sw.js:608 [CONTENT URLS] Direct fetches from /grits/v1/content:
GritsClient-sw.js:615   public/docs/5.3/content/typography: 139.30ms
```

Something like that. What that means, is that we're not going back to the server to check if any of the assets for `typography` have changed in the interim, but we're still assured that they're up to date, because of verification through our own offline Merkle tree. Neat! We have minimal latency (generally speaking no RTTs) in the fresh case, and guaranteed refetches in the stale case.

If you want to set up some nice content to browse through to watch this happening, what I've been testing on is the bootstrap docs:

```
cd
git clone https://github.com/twbs/bootstrap.git
cd bootstrap
npm install
npm run docs-build
cp -r site/dist/* ~/grits/content/public/
```

### Testbed mirror network

Of course, the benefit of doing something like this above something like nginx is still pretty minimal. Cache coherency is nice but the real point is the mirror network. You can start to fool around with that too, if you want to. That's where the advantage of doing something in this fashion starts to really become more significant.

To set the thing up is actually pretty complex, with a bunch of different modules and config elements involved. For now, what's recommended is to do a testbed network.

First, add an NS record indicating that anything under `cache.(your hostname)` is going to be served by the nameserver at your host's IP address. We're authenticating mirrors via TLS and DNS, no different than any other web server, which means this is the simplest way to do it from the mirror-operator's and the browser's perspective.

Next, create a port redirect so our local little DNS server can run.

```
sudo bin/port-helper add -s 53 -d 5353
```

Next, run the testbed.

```
go run cmd/testbed/main.go
```

It'll take a while as it first gets started. It needs to fool with certbot, it runs some self tests to make sure things are working. It's setting up 5 mirror nodes on your machine, with a central origin server. Wait for the log spam to calm down, a few minutes, and it should be good to go.

Now, as you navigate around, you should see something like this in your browser console:

```
[MIRROR STATS] (5 mirrors)
GritsClient-sw.js:634   mirror-0.cache.(your hostname): Latency 431.87 ms | Bandwidth 0.00 KB/s | Reliability 100.00% | Data 335 Bytes
Navigated to https://(your hostname)/docs/5.3/content/typography
GritsClient-sw.js:596 [GRITS STATS] Last 10s: Requests: 10 | Content lookups: 1 (avg 121.20ms) | Blob cache hits: 9 (avg 5.07ms) | Blob cache misses: 0 | Prefetch successes: 5
GritsClient-sw.js:608 [CONTENT URLS] Direct fetches from /grits/v1/content:
GritsClient-sw.js:615   public/docs/5.3/content/typography: 121.10ms
```

It works! What that means is that we have mirrors working, and we've been fetching content partially from the origin server, and partially from the mirror network. A lot of performance and polish work on this is not done, but the fundamentals are there in some form.

In other words, it works on my machine.

### Real mirror network

TODO

## Code layout

### `internal/grits`: Core. Blob storage, configuration, the namespace, and so on. 

* `structures.go` defines core interfaces used throughout
* `blobstore.go` defines the blob storage (mapping of content hash -> actual content)
* `namestore.go` defines the mapping of path names to content hashes

### `internal/gritsd`: Server implementation

* `server.go` defines the actual server implementation.

Almost all the server's functionality is implemented via modules. You will see many of them; of particular importance are:

* `module_http.go` for providing HTTP service
* `module_mount.go` for FUSE mounting
* `module_localvolume.go` for a local writable storage volume

### `grits.cfg`

As mentioned, this is the core config file. Most of it involved configuring particular modules. If you look at the sample config, you'll be able to see the setup of some of the main modules to make a simple instance of the server work:

* `localvolume` is the module creating a local writable volume of storage
* `mount` creates a FUSE mount of a particular volume to a local directory
* `http` creates an HTTP endpoint providing access to the API, and optionally serving some files directly
* `deployment` requests that a particular directory from a particular volume get served in some particular URL space. This is analagous to `location` in nginx. This affects both the `http` module (by mapping the volume-storage space to URL path space), and also the `serviceworker` module (requesting that the service worker do the same on the client side, avoiding RTTs and potentially utilizing other mirrors)
* `serviceworker` sets up necessary server-side stuff to support the service worker

Other modules of note:

* `mirror` - Serve content on behalf of someone else's grits server
* `origin` - Host a network of mirrors

* `peer` - Connect to a network of grits servers to share content
* `tracker` - Host a network of peers

Mirror/origin and peer/tracker are obviously closely related, but for now the base peer-to-peer communication modules are separated from the file mirroring modules.

* `pin` - Configure a "pin" on a particular part of the file space, keep its files always in local storage. This is mostly unused yet, it will become a lot more relevant once remote volumes and sparsely storing namespaces come into play.

### Other useful directories

* `client/` defines vital client-side content. It gets populated into a special read-only volume on startup, so that clients can always access stuff within it.
* `var/` is where all writable data for the server is kept. I think it would be fine (and recommended) to have the entire `grits/` directory outside of `var/` owned by some different user and read-only from the point of view of the server process.
* `testbed/` is where scratch data for all the testbed's mirror servers is kept. This should be safe to blow away if you want to, although that will cause a repeat of the entire slow startup process including requesting all new certs for the testbed mirrors.

## Roadmap

The big roadmap, more or less, is:

* First cut of various core pieces and rough testing (done)
* [Merkle tree](https://en.wikipedia.org/wiki/Merkle_tree) basic storage fundamentals (done)
* FUSE mounting (semi-done)
* Service worker (WIP)
* Remote mounting (todo)
* DHT and node-to-node communication (todo)
* Real production server tooling / testing / configurability / polish (todo)
* Performance (todo)
* Production polish and what's needed for real server operation (todo)

There's a more detailed task list, mostly just internal notes, in TODO.md.

## Obvious questions

### Isn't this IPFS?

Yeah, kind of. I'd like to reuse parts of IPFS, and delvers into the code may have noticed that I'm reusing CID v0 as the format for the content blob addresses. IPFS is *so* heavyweight, though, and its latency seems to be high enough that it is a non-starter for use for individual web assets at the level of a single small file. I've wound up reimplementing a lot of stuff instead of trying to use IPFS's stuff. We're just operating in a different scope.

Specifically, we are *not* attempting massive scale or full peer-to-peer operation, but we *are* attempting to have high performance and lightweight process footprints, so that it's feasible to run the thing in a service worker or on a not-powerful server.

### Won't this be subject to malicious nodes?

Yes. We're only requesting data with a specified known hash, and verifying the hash, so it shouldn't be possible to provide poisoned data real easily, but yes you could mess up the system in other ways. I'm not envisioning everyone in the world being able to run a node in any system; it would be a semi-trusted role which if they're clearly messing up the system then you would boot them out of.

In particular, the current service worker will return an internal server error to the user if some mirror is serving up content that doesn't match the hash it is supposed to have. The intent is that this gets noticed and then handled on the human level, as opposed to at the technical level with malicious nodes being an "expected" happening that the code is coping with.

### Is this secure? Should I turn this on and leave the endpoint up on my production server?

Oh mercy, no. At some point I will do some basic level of security audit to the endpoints but that has not been done. Internal testing only.

### What about performance? Dropped nodes? NAT?

So, all of these are solved problems within IPFS -- my plan is to try to be lightweight with implementation where possible, but there's always the option (particularly with NAT and maintenance of the swarm) to just back up and punt to using IPFS, and focus on the stuff that's genuinely new design ideas. At this point, I've more or less settled on reimplementing most of it, but there's always the option of doing (for example) the blob storage from one of the boxo modules and saving ourselves some getting-it-production-ready pain.

### What about privacy?

Well, you're exposing your IP address to the world if you decide to participate in the swarm. That's potentially an issue. We'll have to be a little careful about who we expose the proxy network to, and to make sure that people are anonymous if they do participate in the proxy network, but even so it'll be something to be careful with.

### How do we incentivize people to contribute resources, and prevent free riders?

So since we're in a relatively small "all friends here" network, we can afford to just have all the nodes blast out data to whoever requests it, and if someone's being obnoxious to the point that it creates an issue then it's dealt with administratively instead of technologically.

## Contributing

If you're interested in trying out running a node once it gets to that point, you can star the repo and I'll send an update, or just send me a note here. In the meantime feel free to check out the code (although, again, it's still super rough) and let me know what you think of the concept or the implementation.

## Enjoy!

Comments? Questions? Feedback? Let me know.