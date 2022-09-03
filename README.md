# bypass4netns: Accelerator for slirp4netns using `SECCOMP_IOCTL_NOTIF_ADDFD` (Kernel 5.9)

bypass4netns is as fast as `--net=host` and _almost_ as secure as traditional slirp4netns.

The current version of bypass4netns needs to be used in conjunction with slirp4netns,
however, future version may work without slirp4netns.

## Benchmark

([Oct 16, 2020](https://github.com/rootless-containers/bypass4netns/tree/0f2633f8c8022d39caacd94372855df401411ae2))

Workload: `iperf3 -c HOST_IP` from `podman run`

- `--net=host` (insecure): 57.9 Gbps
- **bypass4netns**: **56.5 Gbps**
- slirp4netns: 7.56 Gbps

## How it works

bypass4netns eliminates the overhead of slirp4netns by trapping socket syscals and executing them in the host network namespace using
[`SECCOMP_IOCTL_NOTIF_ADDFD`](https://man7.org/linux/man-pages/man2/seccomp_unotify.2.html).

See also the [talks](#talks).

## Requirements
- kernel >= 5.9
- runc >= 1.1 (crun is currently incompatible due to [crun#1002](https://github.com/containers/crun/issues/1002))
- libseccomp >= 2.5
- Rootless Docker, Rootless Podman, or Rootless containerd/nerdctl

Build-time requirement:
- golang >= 1.17

## Compile

```console
make
sudo make install
```

The following binaries will be installed into `/usr/local/bin`:
- `bypass4netns`: the bypass4netns binary.
- `bypass4netnsd`: an optional [REST](./pkg/api/daemon/openapi.yaml) daemon for controlling bypass4netns processes from a non-initial network namespaces. Used by nerdctl.

## Usage
### Hard way (docker|podman|nerdctl)
```console
$ bypass4netns --ignore="127.0.0.0/8,10.0.0.0/8,auto" -p="8080:80"
```

`--ignore=...` is a list of the CIDRs that cannot be bypassed:
- loopback CIDRs (`127.0.0.0/8`)
- slirp4netns CIDR (`10.0.0.0/8`)
- CNI CIDRs inside the slirp's network namespace (`auto`)

```console
$ ./test/seccomp.json.sh >$HOME/seccomp.json
$ $DOCKER run -it --rm --security-opt seccomp=$HOME/seccomp.json --runtime=runc alpine
```

`$DOCKER` is either `docker`, `podman`, or `nerdctl`.

NOTE to Podman users:
crun is currently incompatible due to [crun#1002](https://github.com/containers/crun/issues/1002), and requires removing `sendmsg` from `seccomp.json`.

### Easy way (nerdctl)

bypass4netns is experimentally integrated into nerdctl (>= 0.17.0).

```bash
containerd-rootless-setuptool.sh install-bypass4netnsd
nerdctl run -it --rm -p 8080:80 --label nerdctl/bypass4netns=true alpine
```

NOTE: `--label nerdctl/bypass4netns=true` will be probably replaced with `--security-opt` or something like `--network-opt` in a future version of nerdctl.

## :warning: Caveats :warning:
Accesses to host abstract sockets and host loopback IPs (127.0.0.0/8) from containers are designed to be rejected.

However, it is probably possible to connect to host loopback IPs by exploiting [TOCTOU](https://elixir.bootlin.com/linux/v5.9/source/include/uapi/linux/seccomp.h#L81)
of `struct sockaddr *` pointers.

## TODOs
- Integration for Docker
- Integration for Podman
- Enable to connect to port-fowarded ports from other containers
    - This means that a container with publish option like `-p 8080:80` cannot be connected to port `80` from other containers in the same network namespace
- Handle protocol specific publish option like `-p 8080:80/udp`.
    - Currently, bypass4netns ignores porotocol in publish option.
- Bind port when bypass4netns starts with publish option like `-p 8080:80`
    - Currently, bypass4netns bind socket to port `8080` when it handles bind(2) with target port `80`.
    - bind(2) can fail if other process bind port `8080` before container's process bind port `80`

## Talks
- [Naoki Matsumoto](https://github.com/naoki9911) and [Akihiro Suda](https://github.com/AkihiroSuda).
  Accelerating TCP/IP Communications in Rootless Containers by Socket Switching.
  Presented in [_the 156th meeting of the Special Interest Groups on System Software and Operating System (SIGOS)_](http://www.ipsj.or.jp/sig/os/index.php?2022%C7%AF7%B7%EE%B8%A6%B5%E6%B2%F1),
  SWoPP 2022, Shimonoseki, Japan, July 2022.
    - [Paper (English)](https://pibvt.net/IPSJ-OS22156009.pdf) ([Copyright notice](https://pibvt.net/notice-ipsj.html))
    - [Slides (Japanese)](https://speakerdeck.com/mt2naoki/ip-communications-in-rootless-containers-by-socket-switching)
