#!/bin/bash

set -eu -o pipefail

source ~/.profile

ALPINE_IMAGE="public.ecr.aws/docker/library/alpine:3.16"
nerdctl pull --quiet "${ALPINE_IMAGE}"

SCRIPT_DIR=$(cd $(dirname $0); pwd)
set +u
if [ "$1" == "SYNC" ]; then
  echo "updating source code"
  rm -rf ~/bypass4netns
  sudo cp -r /host ~/bypass4netns
  sudo chown -R ubuntu:ubuntu ~/bypass4netns
  cd ~/bypass4netns
  echo "source code is updated"
  exec $0 "FORK"
  exit 0
fi
cd ~/bypass4netns
rm -f bypass4netns bypass4netnsd
make
sudo make install
set -u
cd $SCRIPT_DIR

set +e
systemctl --user stop run-iperf3
systemctl --user reset-failed
sleep 1
systemctl --user restart containerd
sleep 1
systemctl --user restart buildkit
sleep 3
set -e

systemd-run --user --unit run-iperf3 iperf3 -s
HOST_IP=$(HOST=$(hostname -I); for i in ${HOST[@]}; do echo $i | grep -q "192.168.6."; if [ $? -eq 0 ]; then echo $i; fi; done)
~/bypass4netns/test/seccomp.json.sh | tee /tmp/seccomp.json

sudo journalctl --rotate
sudo journalctl --vacuum-time=1s

echo "===== rootful mode ===="
(
  set +e
  sudo nerdctl rm -f test
  set -ex

  sudo nerdctl run -d --name test $ALPINE_IMAGE sleep infinity
  sudo nerdctl exec test apk add --no-cache iperf3
  sudo nerdctl exec test iperf3 -c $HOST_IP -t 1 --connect-timeout 1000 # it must success to connect.

  sudo nerdctl rm -f test
)

echo "===== static linked binary test ====="
(
  set +e
  systemctl --user stop run-bypass4netns-static
  nerdctl rm -f test1
  nerdctl rm -f test2
  systemctl --user reset-failed
  set -ex

  IMAGE_NAME="b4ns:static"
  nerdctl build -f ./DockerfileHttpServer -t $IMAGE_NAME .

  systemd-run --user --unit run-bypass4netns-static bypass4netns --ignore "127.0.0.0/8,10.0.0.0/8"
  sleep 1
  nerdctl run -d -p 8081:8080 --name test1 $IMAGE_NAME /httpserver -mode server
  nerdctl run --security-opt seccomp=/tmp/seccomp.json -d --name test2 $IMAGE_NAME sleep infinity
  nerdctl exec test2 /httpserver -mode client -url http://$HOST_IP:8081/ping
  nerdctl exec test2 /httpserver -mode client -url http://$HOST_IP:8081/ping
  nerdctl exec test2 /httpserver -mode client -url http://$HOST_IP:8081/ping

  COUNT=$(journalctl --user -u run-bypass4netns-static.service | grep 'bypassed connect socket' | wc -l)
  if [ $COUNT != 3 ]; then
    echo "static linked binary bypassing not working correctly."
    exit 1
  fi

  nerdctl rm -f test1
  nerdctl rm -f test2
  systemctl --user stop run-bypass4netns-static
)

echo "===== '--ignore' option test ====="
(
  set +e
  systemctl --user stop run-bypass4netns
  nerdctl rm -f test
  set -ex

  systemd-run --user --unit run-bypass4netns bypass4netns --ignore "127.0.0.0/8,10.0.0.0/8,192.168.6.0/24" --debug
  nerdctl run --security-opt seccomp=/tmp/seccomp.json -d --name test "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test apk add --no-cache iperf3
  nerdctl exec test iperf3 -c $HOST_IP -t 1
  # TODO: this check is dirty. we want better method to check the connect(2) is ignored.
  journalctl --user -u run-bypass4netns.service | grep "is not bypassed"
  nerdctl rm -f test
  systemctl --user stop run-bypass4netns.service
)

echo "===== connect(2) test ====="
(
  systemd-run --user --unit run-bypass4netns bypass4netns --ignore "127.0.0.0/8,10.0.0.0/8" -p 8080:5201
  set -x
  cd $SCRIPT_DIR
  /bin/bash test_syscalls.sh /tmp/seccomp.json $HOST_IP
  systemctl --user stop run-bypass4netns.service
)

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
  nerdctl run --annotation nerdctl/bypass4netns=true -d -p 8080:5201 --name test1 "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test1 apk add --no-cache iperf3
  TEST1_ADDR=$(nerdctl exec test1 hostname -i)
  systemd-run --user --unit run-test1-iperf3 nerdctl exec test1 iperf3 -s
  nerdctl network create --subnet "10.4.1.0/24" net-2
  nerdctl run --net net-2 --annotation nerdctl/bypass4netns=true -d --name test2 "${ALPINE_IMAGE}" sleep infinity
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
  nerdctl run --annotation nerdctl/bypass4netns=true -d -p 8080:5201 --name test1 "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test1 apk add --no-cache iperf3
  TEST1_ADDR=$(nerdctl exec test1 hostname -i)
  systemd-run --user --unit run-test1-iperf3 nerdctl exec test1 iperf3 -s
  nerdctl network create --subnet "10.4.1.0/24" net-2
  nerdctl run --net net-2 --annotation nerdctl/bypass4netns=true -d --name test2 "${ALPINE_IMAGE}" sleep infinity
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
  nerdctl rm -f test1
  nerdctl rm -f test2
  nerdctl network rm net-2
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://${HOST_IP}:2379 --advertise-client-urls http://${HOST_IP}:2379
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$HOST_IP:2379 --multinode-host-address=$HOST_IP --debug
  sleep 1
  nerdctl run --annotation nerdctl/bypass4netns=true -d -p 8080:5201 --name test1 "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test1 apk add --no-cache iperf3
  TEST1_ADDR=$(nerdctl exec test1 hostname -i)
  systemd-run --user --unit run-test1-iperf3 nerdctl exec test1 iperf3 -s
  nerdctl network create --subnet "10.4.1.0/24" net-2
  nerdctl run --net net-2 --annotation nerdctl/bypass4netns=true -d --name test2 "${ALPINE_IMAGE}" sleep infinity
  nerdctl exec test2 apk add --no-cache iperf3
  nerdctl exec test2 iperf3 -c $TEST1_ADDR -t 1 --connect-timeout 1000 # it must success to connect.

  nerdctl rm -f test1
  nerdctl rm -f test2
  nerdctl network rm net-2
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
)

echo "===== nested netns test ===="
(
  CONTAINER_NAME="test-nested"
  set +e
  nerdctl rm -f $CONTAINER_NAME
  systemctl --user stop run-iperf3
  systemctl --user stop run-bypass4netnsd
  systemctl --user reset-failed
  set -ex


  IMAGE_NAME="b4ns:nested"
  nerdctl build -f ./DockerfileNestedNetNS -t $IMAGE_NAME .

  systemd-run --user --unit run-bypass4netnsd bypass4netnsd
  sleep 1
  nerdctl run --privileged --annotation nerdctl/bypass4netns=true -d -p 5202:5201 --name $CONTAINER_NAME $IMAGE_NAME sleep infinity

  # with container's netns
  systemd-run --user --unit run-iperf3 nerdctl exec $CONTAINER_NAME iperf3 -s
  sleep 1
  iperf3 -c localhost -t 1 -p 5202 --connect-timeout 1000 # it must success to connect.
  systemctl --user stop run-iperf3
  systemctl --user reset-failed

  # with nested netns
  nerdctl exec $CONTAINER_NAME mkdir /sys2
  nerdctl exec $CONTAINER_NAME mount -t sysfs --make-private /sys2
  nerdctl exec $CONTAINER_NAME ip netns add nested
  systemd-run --user --unit run-iperf3 nerdctl exec $CONTAINER_NAME ip netns exec nested iperf3 -s
  sleep 1
  set +e
  iperf3 -c localhost -t 1 -p 5202 --connect-timeout 1000 # it must fail
  if [ $? -eq 0 ]; then
    echo "iperf3 must not success to connect."
    exit 1
  fi
  set -e
  systemctl --user stop run-iperf3

  nerdctl rm -f test-nested
  systemctl --user reset-failed
)
