# grits

Grits is load-sharing software, designed to allow a community-supported web site to operate based on direct contributions of resources by the members of the web site. A site using grits proxies should be able to shift a lot of the load of operating the site onto the members of the community by having them run fairly simple software, while still operating in a fast and secure manner.

You can read more about the motivations behind the software, and future directions it might take after this stage, [on the Lemmy post](https://lemmy.world/post/62323). The short summary is that for at least 20 years, people have been talking about switching to a more peer-to-peer vision of the internet, but the actual success that's been demonstrated has always been pretty limited in scope compared to the non-peer-to-peer options. Bittorrent is great, ActivityPub is great but has caveats, but Wikipedia still runs on fairly expensive centrally-served hosting. My vision would be that it becomes realistic to run a busy Mastadon node, or a site like Wikipedia, and have a substantial amount of the hosting being done by the users.

The way that happens is that when you request e.g. a Mastadon page, the server gives you a little snippet of Javascript that establishes connections to some nodes in a content-addressable store operated by the community, and it can request from those nodes a lot of the data that the web app needs (verifying the hashes). Thus the central server has a lot less load.

## Current Status and Roadmap

This software is in very early stage at this point. At present, the proxies can talk to one another and exchange files in a testbed, and that's about it. Making it work on the actual real-world internet is another story; the work currently in progress is to handle congestion and packet loss, switching to a new proxy smoothly if one is performing badly, etc, work well. That's not working yet but seems likely within the somewhat-near future.

Note that I'm reimplementing the transport layer, instead of using e.g. IPFS or Hypercore, which adds a ton of work and risk. I talk a little more on Lemmy about the reasons for this surprising decision; we'll see if it turns out to have been a good idea.

The roadmap in order indicating the early stage we're currently at, is:

* Basic peer communication
* DHT and file finding / sending / receiving
* Coping with congestion and real-world internet constraints (WE ARE HERE)
* Real internet testing, NAT traversal, security
* Integrate with a real web app and test serving real content via the network
* Real load testing, probing for places where the scalability can be improved
* Encryption and real security for the served content

We'll probably reevaluate a lot once it gets to that point, but there are also some more pie-in-the-sky features planned some time after that. Some of them include:

* Better support for database-like content in the shared store (better support for tiny data, maybe something like Holepunch's B-tree shared filesystem, support for indexing and searching)
* Support in a secure fashion for writes to the shared store from non-central nodes
* Support for trusted content, PKI, signing and encrypting data in the network

## Contributing

If you're interested to be part of the real-internet test network once things get to that stage, drop me a line. Also, obviously if you're interested in be involved in the code side of things, definitely drop me a line as I'd love the help. Prerequisities at present are:

* Node.js
* npm
* A collection of dummy files to populate the test network's storage with (around 1000 is probably good -- they can be literally any random files)

To test out the current (early!) stage of the code, clone the repo, and then:

```
npm install
cp -r (some large collection of dummy files) test-images/
mkdir tmp-download
npx ts-node src/run-test-0.ts
```

That'll start up a little network of 50 nodes, with 10 random files from `test-images/` populated into each one of them. Once they've had 30-40 seconds to sort themselves out, you should be able to run a command similar to:

```
wget http://localhost:1234/blob/1cbff4192356c731e2e75dc26dd124170523abd99f15d8902938a9cefe5ec4a0:594725
```

wget should successfully download a file which was shared and then transferred over the grits network. You'll have to replace that sha-256 hash and length with an actual one; you can pick some random file out of `grits-test-run/2` and, with its filename and size, run `wget http://localhost:1234/blob/$sha:$size`.

The next step forward -- success on a degraded network, with bandwidth limits and packet loss and etc -- is going to be when `src/run-test-1.ts` starts working.

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
