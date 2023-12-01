#!/bin/bash

set -eu -o pipefail

source ~/.profile

ALPINE_IMAGE="public.ecr.aws/docker/library/alpine:3.16"
nerdctl pull --quiet "${ALPINE_IMAGE}"

HOST_IP=$(HOST=$(hostname -I); for i in ${HOST[@]}; do echo $i | grep -q "192.168.6."; if [ $? -eq 0 ]; then echo $i; fi; done)
systemd-run --user --unit run-iperf3 iperf3 -s

echo "===== Benchmark: netns -> host With bypass4netns ====="
(
  set +e
  nerdctl rm -f test
  systemctl --user stop run-bypass4netnsd
  systemctl --user reset-failed
  set -ex

  # start bypass4netnsd for nerdctl integration
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd
  sleep 1
  nerdctl run --label nerdctl/bypass4netns=true -d --name test "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test apk add --no-cache iperf3
  nerdctl exec test iperf3 -c $HOST_IP
  nerdctl rm -f test
)

echo "===== Benchmark: netns -> host Without bypass4netns (for comparison) ====="
(
  set +e
  nerdctl rm -f test
  set -ex

  nerdctl run -d --name test "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test apk add --no-cache iperf3
  nerdctl exec test iperf3 -c $HOST_IP
  nerdctl rm -f test
)

echo "===== Benchmark: host -> netns With bypass4netns ====="
(
  set +e
  nerdctl rm -f test
  systemctl --user stop run-iperf3-netns
  systemctl --user reset-failed
  set -ex

  nerdctl run --label nerdctl/bypass4netns=true -d --name test -p 8080:5201 "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test apk add --no-cache iperf3
  systemd-run --user --unit run-iperf3-netns nerdctl exec test iperf3 -s -4
  sleep 1 # waiting `iperf3 -s -4` becomes ready
  iperf3 -c $HOST_IP -p 8080
  nerdctl rm -f test
)

echo "===== Benchmark: host -> netns Without bypass4netns (for comparison) ====="
(
  set +e
  nerdctl rm -f test
  systemctl --user stop run-iperf3-netns2
  systemctl --user reset-failed
  set -ex

  nerdctl run -d --name test -p 8080:5201 "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test apk add --no-cache iperf3
  systemd-run --user --unit run-iperf3-netns2 nerdctl exec test iperf3 -s -4
  sleep 1
  iperf3 -c $HOST_IP -p 8080
  nerdctl rm -f test
)
