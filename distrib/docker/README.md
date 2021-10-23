# Ergo Docker

This folder holds Ergo's Dockerfile and related materials. Ergo is published automatically to Docker Hub at
[ergochat/ergo](https://hub.docker.com/r/ergochat/ergo).

The `latest` tag tracks the `stable` branch of Ergo, which contains the latest stable release. The `dev` tag tracks the
master branch, which may by unstable and is not recommended for production.

## Quick start

The Ergo docker image is designed to work out of the box - it comes with a usable default config and will automatically
generate self-signed TLS certificates. To get a working ircd, all you need to do is run the image and expose the ports:

```shell
docker run --name ergo -d -p 6667:6667 -p 6697:6697 ergochat/ergo:tag
```

This will start Ergo and listen on ports 6667 (plain text) and 6697 (TLS). The first time Ergo runs it will create a
config file with a randomised oper password. This is output to stdout, and you can view it with the docker logs command:

```shell
# Assuming your container is named `ergo`; use `docker container ls` to
# find the name if you're not sure.
docker logs ergo
```

You should see a line similar to:

```
Oper username:password is admin:cnn2tm9TP3GeI4vLaEMS
```

## Persisting data

Ergo has a persistent data store, used to keep account details, channel registrations, and so on. To persist this data
across restarts, you can mount a volume at /ircd.

For example, to create a new docker volume and then mount it:

```shell
docker volume create ergo-data
docker run -d -v ergo-data:/ircd -p 6667:6667 -p 6697:6697 ergochat/ergo:tag
```

Or to mount a folder from your host machine:

```shell
mkdir ergo-data
docker run -d -v $(PWD)/ergo-data:/ircd -p 6667:6667 -p 6697:6697 ergochat/ergo:tag
```

## Customising the config

Ergo's config file is stored at /ircd/ircd.yaml. If the file does not exist, the default config will be written out. You
can copy the config from the container, edit it, and then copy it back:

```shell
# Assuming that your container is named `ergo`, as above.
docker cp ergo:/ircd/ircd.yaml .
vim ircd.yaml # edit the config to your liking
docker cp ircd.yaml ergo:/ircd/ircd.yaml
```

You can use the `/rehash` command to make Ergo reload its config, or send it the HUP signal:

```shell
docker kill -HUP ergo
```

## Using custom TLS certificates

TLS certs will by default be read from /ircd/tls.crt, with a private key in /ircd/tls.key. You can customise this path
in the ircd.yaml file if you wish to mount the certificates from another volume. For information on using Let's Encrypt
certificates, see
[this manual entry](https://github.com/ergochat/ergo/blob/master/docs/MANUAL.md#using-valid-tls-certificates).

## Using docker-compose

This folder contains a sample docker-compose file which can be used to start an Ergo instance with ports exposed and
data persisted in a docker volume. Simply download the file and then bring it up:

```shell
curl -O https://raw.githubusercontent.com/ergochat/ergo/master/distrib/docker/docker-compose.yml
docker-compose up -d
```

## Building

If you wish to manually build the docker image, you need to do so from the root of the Ergo repository (not
the `distrib/docker` directory):

```shell
docker build .
```
