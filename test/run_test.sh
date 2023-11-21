#!/bin/bash

set -eu -o pipefail

ALPINE_IMAGE="public.ecr.aws/docker/library/alpine:3.16"
nerdctl pull --quiet "${ALPINE_IMAGE}"

SCRIPT_DIR=$(cd $(dirname $0); pwd)
cd $SCRIPT_DIR

echo "===== '--ignore' option test ====="
(
  set -x
  systemd-run --user --unit run-bypass4netns bypass4netns --ignore "127.0.0.0/8,10.0.0.0/8,192.168.6.0/24" --debug
  nerdctl run --security-opt seccomp=/tmp/seccomp.json -d --name test "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test apk add --no-cache iperf3
  nerdctl exec test iperf3 -c $(cat /tmp/host_ip) -t 1
  # TODO: this check is dirty. we want better method to check the connect(2) is ignored.
  journalctl --user -u run-bypass4netns.service | grep "is not bypassed"
  nerdctl rm -f test
  systemctl --user stop run-bypass4netns.service
)

# nerdctl image build not working.
#[+] Building 10.1s (2/2) FINISHED                                                            
# => [internal] load build definition from Dockerfile                                    0.0s
# => => transferring dockerfile: 274B                                                    0.0s
# => ERROR [internal] load metadata for public.ecr.aws/docker/library/alpine:3.16       10.0s
#------
# > [internal] load metadata for public.ecr.aws/docker/library/alpine:3.16:
#------
#Dockerfile:1
#--------------------
#   1 | >>> FROM public.ecr.aws/docker/library/alpine:3.16
#   2 |     
#   3 |     RUN apk add python3
#--------------------
#error: failed to solve: public.ecr.aws/docker/library/alpine:3.16: failed to do request: Head "https://public.ecr.aws/v2/docker/library/alpine/manifests/3.16": dial tcp: lookup public.ecr.aws on 10.0.2.3:53: read udp 10.0.2.100:47105->10.0.2.3:53: i/o timeout
#echo "===== connect(2),sendto(2) test ====="
#(
#  systemd-run --user --unit run-bypass4netns bypass4netns --ignore "127.0.0.0/8,10.0.0.0/8" -p 8080:5201
#  set -x
#  cd $SCRIPT_DIR/test
#  /bin/bash test_syscalls.sh /tmp/seccomp.json $(cat /tmp/host_ip)
#  systemctl --user stop run-bypass4netns.service
#)

echo "===== Test bypass4netnsd ====="
(
 set -x
 source ~/.profile
 ./test_b4nnd.sh
)

echo "===== Benchmark: netns -> host With bypass4netns ====="
(
 set -x

 # start bypass4netnsd for nerdctl integration
 systemd-run --user --unit run-bypass4netnsd bypass4netnsd
 sleep 1
 nerdctl run --label nerdctl/bypass4netns=true -d --name test "${ALPINE_IMAGE}" sleep infinity
 nerdctl exec test apk add --no-cache iperf3
 nerdctl exec test iperf3 -c "$(cat /tmp/host_ip)"
 nerdctl rm -f test
)

echo "===== Benchmark: netns -> host Without bypass4netns (for comparison) ====="
(
 set -x
 nerdctl run -d --name test "${ALPINE_IMAGE}" sleep infinity
 nerdctl exec test apk add --no-cache iperf3
 nerdctl exec test iperf3 -c "$(cat /tmp/host_ip)"
 nerdctl rm -f test
)

echo "===== Benchmark: host -> netns With bypass4netns ====="
(
 set -x
 nerdctl run --label nerdctl/bypass4netns=true -d --name test -p 8080:5201 "${ALPINE_IMAGE}" sleep infinity
 nerdctl exec test apk add --no-cache iperf3
 systemd-run --user --unit run-iperf3-netns nerdctl exec test iperf3 -s -4
 sleep 1 # waiting `iperf3 -s -4` becomes ready
 iperf3 -c "$(cat /tmp/host_ip)" -p 8080
 nerdctl rm -f test
)

echo "===== Benchmark: host -> netns Without bypass4netns (for comparison) ====="
(
 set -x
 nerdctl run -d --name test -p 8080:5201 "${ALPINE_IMAGE}" sleep infinity
 nerdctl exec test apk add --no-cache iperf3
 systemd-run --user --unit run-iperf3-netns2 nerdctl exec test iperf3 -s -4
 sleep 1
 iperf3 -c "$(cat /tmp/host_ip)" -p 8080
 nerdctl rm -f test
)