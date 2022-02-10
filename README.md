# bypass4netns: Accelerator for slirp4netns using `SECCOMP_IOCTL_NOTIF_ADDFD` (Kernel 5.9)

bypass4netns is as fast as `--net=host` and _almost_ as secure as traditional slirp4netns.

The current version of bypass4netns needs to be used in conjunction with slirp4netns,
however, future version may work without slirp4netns.

The project name is still subject to change.

## Benchmark

([Oct 16, 2020](https://github.com/rootless-containers/bypass4netns/tree/0f2633f8c8022d39caacd94372855df401411ae2))

Workload: `iperf3 -c HOST_IP` from `podman run`

- `--net=host` (insecure): 57.9 Gbps
- **bypass4netns**: **56.5 Gbps**
- slirp4netns: 7.56 Gbps

## How it works

To be documented. See the code :)

## Requirements
- kernel >= 5.9
- runc >= 1.1
- libseccomp >= 2.5
- Rootless Docker, Rootless Podman, or Rootless containerd/nerdctl

Build-time requirement:
- golang >= 1.17

## Compile

```console
make
sudo make install
```

## Usage
### with official package (docker|podman|nerdctl)
```console
$ bypass4netns --ignore="127.0.0.0/8,10.0.0.0/8" -p="8080:80"
```

```console
$ ./test/seccomp.json.sh >$HOME/seccomp.json
$ $DOCKER run -it --rm --security-opt seccomp=$HOME/seccomp.json alpine
```

`$DOCKER` is either `docker`, `podman`, or `nerdctl`.

### with patched nerdctl(experimental)

Experimentally, nerdctl patched for bypass4netns is available at [naoki9911/nerdctl](https://github.com/naoki9911/nerdctl/tree/bypass4netns-dev)

To use this, clone patched nerdctl at the same directory of bypass4netns and build.
```console
~/bypass4netns$ pwd
/home/$USER/bypass4netns
~/bypass4netns$ cd ..
~$ git clone -b bypass4netns-dev https://github.com/naoki9911/nerdctl
~$ cd nerdctl
~/nerdctl$ make
```

```console
$ bypass4netnsd
```

```console
~/nerdctl$ _output/nerdctl run -it --rm -p 8080:80 --label nerdctl/bypass4netns=true alpine
```

## :warning: Caveats :warning:
Accesses to host abstract sockets and host loopback IPs (127.0.0.0/8) from containers are designed to be rejected.

However, it is probably possible to connect to host loopback IPs by exploiting [TOCTOU](https://elixir.bootlin.com/linux/v5.9/source/include/uapi/linux/seccomp.h#L81)
of `struct sockaddr *` pointers.

## TODOs
- Accelerate port forwarding (`(docker|podman) run -p`) as well
- Enable to connect to port-fowarded ports from other containers
    - This means that a container with publish option like `-p 8080:80` cannot be connected to port `80` from other containers in the same network namespace
- Handle protocol specific publish option like `-p 8080:80/udp`.
    - Currently, bypass4netns ignores porotocol in publish option.
- Bind port when bypass4netns starts with publish option like `-p 8080:80`
    - Currently, bypass4netns bind socket to port `8080` when it handles bind(2) with target port `80`.
    - bind(2) can fail if other process bind port `8080` before container's process bind port `80`
