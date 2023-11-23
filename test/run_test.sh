#!/bin/bash

set -eu -o pipefail

source ~/.profile

ALPINE_IMAGE="public.ecr.aws/docker/library/alpine:3.16"
nerdctl pull --quiet "${ALPINE_IMAGE}"

SCRIPT_DIR=$(cd $(dirname $0); pwd)
cd $SCRIPT_DIR

set +u

if [ ! -v 1 ]; then
  echo "COPY"
  rm -rf ~/bypass4netns
  sudo cp -r /host ~/bypass4netns
  sudo chown -R ubuntu:ubuntu ~/bypass4netns
  cd ~/bypass4netns
  exec $0 "FORK"
  exit 0
fi

set -u

echo "THIS IS FORK"

cd ~/bypass4netns
rm -f bypass4netns bypass4netnsd
make
sudo make install
cd $SCRIPT_DIR

set +e
systemctl --user stop run-iperf3
systemctl --user reset-failed
sleep 1
set -e

systemd-run --user --unit run-iperf3 iperf3 -s

echo "===== '--ignore' option test ====="
(
  set +e
  systemctl --user stop run-bypass4netns
  nerdctl rm -f test
  set -ex

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

echo "===== tracer test (disabled) ====="
(
  set +e
  systemctl --user stop run-bypass4netnsd
  nerdctl rm -f test1
  nerdctl rm -f test2
  nerdctl network rm net-2
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --handle-c2c-connections=true
  sleep 1
  nerdctl run --label nerdctl/bypass4netns=true -d -p 8080:5201 --name test1 "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test1 apk add --no-cache iperf3
  TEST1_ADDR=$(nerdctl exec test1 hostname -i)
  systemd-run --user --unit run-test1-iperf3 nerdctl exec test1 iperf3 -s
  nerdctl network create --subnet "10.4.1.0/24" net-2
  nerdctl run --net net-2 --label nerdctl/bypass4netns=true -d --name test2 "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test2 apk add --no-cache iperf3
  nerdctl exec test2 iperf3 -c $TEST1_ADDR -t 1 --connect-timeout 1000 # it must success to connect.

  nerdctl rm -f test1
  nerdctl rm -f test2
  nerdctl network rm net-2
  systemctl --user stop run-bypass4netnsd
)

echo "===== tracer test (enabled) ====="
(
  set +e
  systemctl --user stop run-bypass4netnsd
  nerdctl rm -f test1
  nerdctl rm -f test2
  nerdctl network rm net-2
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --handle-c2c-connections=true --tracer=true --debug
  sleep 1
  nerdctl run --label nerdctl/bypass4netns=true -d -p 8080:5201 --name test1 "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test1 apk add --no-cache iperf3
  TEST1_ADDR=$(nerdctl exec test1 hostname -i)
  systemd-run --user --unit run-test1-iperf3 nerdctl exec test1 iperf3 -s
  nerdctl network create --subnet "10.4.1.0/24" net-2
  nerdctl run --net net-2 --label nerdctl/bypass4netns=true -d --name test2 "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test2 apk add --no-cache iperf3
  set +e
  nerdctl exec test2 iperf3 -c $TEST1_ADDR -t 1 --connect-timeout 1000 # it must not success to connect.
  if [ $? -eq 0 ]; then
    echo "tracer seems not working"
    exit 1
  fi
  set -e

  nerdctl rm -f test1
  nerdctl rm -f test2
  nerdctl network rm net-2
  systemctl --user stop run-bypass4netnsd
)


echo "===== multinode test (single node) ===="
(
  set +e
  systemctl --user stop run-bypass4netnsd
  nerdctl rm -f test1
  nerdctl rm -f test2
  nerdctl network rm net-2
  systemctl --user reset-failed
  set -ex

  HOST_IP=$(hostname -I | sed 's/ //')
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$HOST_IP:2379 --multinode-host-address=$HOST_IP --debug
  sleep 1
  nerdctl run --label nerdctl/bypass4netns=true -d -p 8080:5201 --name test1 "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test1 apk add --no-cache iperf3
  TEST1_ADDR=$(nerdctl exec test1 hostname -i)
  systemd-run --user --unit run-test1-iperf3 nerdctl exec test1 iperf3 -s
  nerdctl network create --subnet "10.4.1.0/24" net-2
  nerdctl run --net net-2 --label nerdctl/bypass4netns=true -d --name test2 "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test2 apk add --no-cache iperf3
  # wait the key is propagated to etcd
  # TODO: why it takes so much time?
  nerdctl exec test2 iperf3 -c $TEST1_ADDR -t 1 --connect-timeout 1000 # it must success to connect.

  nerdctl rm -f test1
  nerdctl rm -f test2
  nerdctl network rm net-2
  systemctl --user stop run-bypass4netnsd
)

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
  nerdctl exec test iperf3 -c "$(cat /tmp/host_ip)"
  nerdctl rm -f test
)

echo "===== Benchmark: netns -> host Without bypass4netns (for comparison) ====="
(
  set +e
  nerdctl rm -f test
  set -ex

  nerdctl run -d --name test "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test apk add --no-cache iperf3
  nerdctl exec test iperf3 -c "$(cat /tmp/host_ip)"
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
  iperf3 -c "$(cat /tmp/host_ip)" -p 8080
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
  iperf3 -c "$(cat /tmp/host_ip)" -p 8080
  nerdctl rm -f test
)