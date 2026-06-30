# Installing Grits / Gimbal

Gimbal (the frontend) and Grits (the backend) currently ship together in one git repo.

## Build

This only works on Linux right now.

**Prerequisites:**

* Go >= 1.25.11
* `sudo apt install fuse3 certbot npm` (or equivalent)

**Note:** `make deps` will install `govulncheck` in order to run `make audit`.

```bash
git clone https://gimbal.melanic.org/src/.git grits
cd grits
make test      # backend smoke tests
make deps      # fetch JS and Go dependencies
make           # build the server
```

Note that the initial `git clone` is slow — it's using bare HTTP against the self-hosted store rather than a git-specific protocol. This is intentional (no git-specific code in the backend, no Github dependency), but it's not ideal in practice.

## Configure

```bash
cp sample-config.json config.json
$EDITOR config.json
```

Replace `%USER%` with your Unix username and `%EMAIL%` with your email (used by certbot for automatic HTTPS certificate provisioning).

## Run

```bash
sudo bin/gritsd
```

The server drops privileges to your configured user as soon as it's opened the ports it needs. To run without `sudo`, configure a port above 1024, run certbot manually, and omit `sudo`.

## Initial Setup

Once the server is running, the FUSE mount at `mnt/` gives you direct access to the file store.

**Shutdown note:** If the server is shut down while something in the FUSE mount is open, it'll wait rather than leave a stale mount. Close any open files, `cd` out of the mount, unmount manually, and the shutdown will complete.

**Auto-import note:** The sample config automatically imports `client/` into the live directory for `gimbal.{your domain}.com` on every restart. Edits to `client/` in your checkout will overwrite local changes in the store. Other vhosts — including user-created clones — are not affected.

Populate the store with the one-time skeleton import:

```bash
cp -r content/skel/* content/skel/.grits mnt/
```

Then create users:

```bash
bin/grits adduser glenda
bin/grits adduser {your username}
```

Set up a DNS entry for `gimbal.{your domain}.com`. For a real deployment, a wildcard `*.{your domain}.com` is recommended — a lot of the value here comes from being able to add new vhosts cheaply.

## Test

Log in at `https://gimbal.{your domain}.com/` and run the self-tests:

```js
gimbal.login({g:1})
gimbal.test()
```

Frontend tests will take a while.

## Web Serving

Once tests pass, you can start putting up content. `gimbal.upload()` and `gimbal.path('/path').unzip()` are useful. The copy-on-write deployment workflow looks like this:

```js
gimbal.path('/sites/{hostname}/dev/v1').mkdir({p:1})
gimbal.path('/sites/{hostname}/dev/v1/index.html').w('version 1')
gimbal.path('/sites/{hostname}/dev/v1').ln(gimbal.path('/sites/{hostname}/live'), {ff:1})
```

(`ff` forcibly overwrites `live` with a copy of `dev/v1`, instead of the normal behavior of creating a new entry inside an existing directory.)

To start a v2:

```js
gimbal.path('/sites/{hostname}/dev/v1').ln(gimbal.path('/sites/{hostname}/dev/v2'), {ff:1})
```

You can deploy `dev/v2` to a temporary vhost to test against real auth and real data before cutting it over to `live`.

## Backend Administration

Useful commands:

```bash
bin/grits ping                              # test that the cmdline module is working
bin/grits import local/path //vol/dest      # import files from your Linux filesystem
bin/grits adduser username                  # add a user (prompts for password)
bin/grits deluser username                  # delete a user
```

The intent is that most administration will eventually happen from inside Gimbal — HTTP logs, user management, and so on. Some bootstrapping will always require backend commands.

Making `gritsd` a systemctl service is on the to-do list.