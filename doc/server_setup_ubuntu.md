# Running `upspinserver` on Ubuntu 16.04

These instructions are part of the instructions for
[Setting up `upspinserver`](/doc/server_setup.md).
Please make sure you have read that document first.

## Introduction

These instructions assume you have access to an Debian or Ubuntu linux
server, and that the server is reachable at your chosen host name.
(`upspin.example.com`)

> Note that these instructions have been verified to work against Ubuntu 16.04.
> The exact commands may differ on your system.

Once the server is running you should log in to it as root and configure it to
run `upspinserver` by following these instructions.

## Create a user for `upspinserver`

The following commands must be executed on the server as the super user, `root`,
perhaps via `sudo su`.

Throughout this document, we will mark commands to be run on your
local machine with the shell prompt `local$` and commands to be
run on your server with `server%`, or if running as `root`, `server#`.

Create a Unix account named `upspin`:

```
server# useradd -m upspin
```

Give yourself SSH access to the `upspin` account on the server (a convenience):

```
server# su upspin
server% cd $HOME
server% mkdir .ssh
server% chmod 0700 .ssh
server% cat > .ssh/authorized_keys
(Paste your SSH public key here and type Control-D and Enter)
```

## Build `upspinserver` and copy it to the server

From your workstation, run these commands:

```
local$ GOOS=linux GOARCH=amd64 go build upspin.io/cmd/upspinserver
local$ scp upspinserver upspin@upspin.example.com:.
```

## Run `upspinserver` on server startup

The following commands must be executed on the server as the super user, `root`.

These instructions assume that your Linux server is running `systemd`.

Create the file `/etc/systemd/system/upspinserver.service` that contains
the following service definition.

```
[Unit]
Description=Upspin server

[Service]
ExecStart=/home/upspin/upspinserver
User=upspin
Group=upspin
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

### Allow `upspinserver` to listen on port `443`

The `upspinserver` binary needs to listen on port `443` in order to obtain
its TLS certificates through [LetsEncrypt](https://letsencrypt.org/).

Normally only user `root` can bind ports below `1024`.
Instead of running `upspinserver` as `root` (which is generally discouraged),
we will grant the `upspinserver` binary this capability by using `setcap` (as
`root`):

```
server# setcap cap_net_bind_service=+ep /home/upspin/upspinserver
```

Note that you need to run this `setcap` command whenever the `upspinserver`
binary is updated.

### Start the service

Use `systemctl` to enable and start the service:

```
server# systemctl enable --now /etc/systemd/system/upspinserver.service
```

You may also use `systemctl stop upspinserver` and `systemctl restart
upspinserver` to stop and restart the server, respectively.

You can use `journalctl` to see the log output of the server:

```
server# journalctl -f -u upspinserver

```

## Continue

You can now continue following the instructions in
[Setting up `upspinserver`](/doc/server_setup.md).
