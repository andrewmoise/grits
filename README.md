# grits

Grits is load-sharing software, designed to allow a community-supported web site to operate based on contributions of resources by the members of the web site. A site using grits proxies should be able to shift a lot of the load of operating the site onto the members of the community by having them run fairly simple software, while still operating in a fast and secure manner.

In its current imagined incarnation, it's a network of peer-to-peer caching proxies that serve a content-addressable data store on behalf of the central server. Clients can read data from proxies operated by members of the community, as opposed to placing load on the central server, enabling people to host a community web site without taking on as much of a financial and technological burden.

You can read more about the motivations behind the software, and future directions it might take after this stage, [on the Lemmy post](https://lemmy.world/post/62323).

## Current Status

At present, the proxies can talk to one another and exchange files. Making it work on the actual real-world internet is another story; the work currently in progress is to handle congestion and packet loss and switching to a new proxy, etc, work well. That's not working yet but seems likely within the somewhat-near future.

## Prerequisites

The project is Typescript running on Node.JS. To try it out, you'll need:

* Node.js
* npm
* A collection of dummy files to populate the test network's storage with (around 1000 is probably good -- they can be literally any random files)

## How To

Again, it doesn't do much yet. But, if you want to examine the code or run the test network, definitely check it out and play with it! You can clone the repo and do:

```
npm install
cp -r (some large collection of dummy files) test-images/
mkdir tmp-download
npx ts-node src/run-test-0.ts
```

That'll start up a little network of 50 nodes, with 10 random files from `test-images/` populated into each one of them. Once they've had a little bit of time to communicate with one another, you should be able to run a command similar to:

```
wget http://localhost:1234/blob/1cbff4192356c731e2e75dc26dd124170523abd99f15d8902938a9cefe5ec4a0:594725
```

And get back a file which was shared and then downloaded over the network. You'll have to replace that sha-256 hash and length with an actual one; you can pick some random file out of `grits-test-run/2` and paste in its filename and size in order for the `wget` call to actually work.

The next step forward -- working on a degraded network, with bandwidth limits and packet loss and etc -- is going to be when `src/run-test-1.ts` starts working.

## Code structure

The main structure of classes and how they collaborate with one another is:

* ProxyManager (from `proxy.ts`) is the top level class for a host within the network
  * FileCache (from `filecache.ts`) is where it keeps its data on the local storage
  * Config (from `config.ts`) defines top-level parameters for the host
  * RootProxyManager, also from `proxy.ts`, defines special functionality for the trusted "root" proxy within the network

* `structures.ts` defines some data classes
  * PeerProxy is an abstraction for some other host within the network
  * CachedFile is an abstraction for a file in our local storage

* NetworkManager (from `network.ts`) operates the UDP sockets
* Message and its subclasses (from `messages.ts`) represent messages and define their serialization and deserialization
* DownloadManager (from `download.ts`) operates any downloads in progress
* UpstreamManager and DownstreamManager (from `traffic.ts`) handle traffic shaping and congestion management
* HttpServer (from `web.ts`) runs the web server
* BlobFinder (from `dht.ts`) defines the logic for locating files within the DHT

## Enjoy!

Comments? Questions? Feedback? Let me know.
