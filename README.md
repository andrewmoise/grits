=== grits

Grits is load-sharing software, designed to allow a community-supported web site to operate based on contributions of resources by the members of the web site. A site using grits proxies should be able to shift a lot of the load of operating the site onto the members of the community by having them run fairly simple software, while still operating in a fast and secure manner.

In its current imagined incarnation, it's a network of peer-to-peer caching proxies that serve a content-addressable data store on behalf of the central server. Clients can read data from proxies operated by members of the community, as opposed to placing load on the central server, enabling people to host a community web site without taking on as much of a financial and technological burden.

You can read more about the motivations behind the software, and future directions it might take after this stage, [on the Lemmy post](https://lemmy.world/post/62323).

=== Current State

In the current state, you can start a test network and watch it send messages to itself, and that's about it. The current milestone is to aim for the network being able to find content over the DHT and serve it, and with some prototypical version of fast fetching of content. That milestone should be possible in a week or two. Once that's working then the next step will be robustness, security, and general work that's needed for it to be operational in the real world, and then some real world testing.

=== Prerequisites

To try it out, you'll need:

* Node.js
* npm
* A collection of dummy files to populate the test network's storage with (around 1000 is probably good -- they can be literally any random files)

=== How To

Again, it doesn't do much yet. But, if you want to examine the code or run the test network, definitely check it out and play with it! It's based on Typescript and Node.JS. You can clone the repo and do:

```
npm install
cp -r (some large collection of dummy files) test-images/
mkdir tmp-download
./run-test-network.sh
```

That'll start a test network of 50 nodes, distribute files from the dummy file folder among the proxies, and send heartbeats around. As of this writing, it's not able to actually find the content or serve it quite yet.

=== Enjoy!

Comments? Questions? Feedback? Let me know.
