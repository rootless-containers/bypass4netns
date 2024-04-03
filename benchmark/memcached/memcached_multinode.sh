#!/bin/bash

cd $(dirname $0)
. ../../test/util.sh

set +e
NAME="test" exec_lxc sudo nerdctl rm -f memcached-server
NAME="test" exec_lxc nerdctl rm -f memcached-server
sudo lxc rm -f test2

TEST1_VXLAN_MAC="02:42:c0:a8:00:1"
TEST1_VXLAN_ADDR="192.168.2.1"
TEST2_VXLAN_MAC="02:42:c0:a8:00:2"
TEST2_VXLAN_ADDR="192.168.2.2"
MEMCACHED_VERSION=1.6.22
MEMCACHED_IMAGE="memcached:${MEMCACHED_VERSION}"
MEMTIRE_VERSION=2.0.0
MEMTIRE_IMAGE="redislabs/memtier_benchmark:${MEMTIRE_VERSION}"

set -eux -o pipefail

NAME="test" exec_lxc sudo nerdctl pull --quiet $MEMCACHED_IMAGE
NAME="test" exec_lxc nerdctl pull --quiet $MEMCACHED_IMAGE
NAME="test" exec_lxc sudo nerdctl pull --quiet $MEMTIRE_IMAGE
NAME="test" exec_lxc nerdctl pull --quiet $MEMTIRE_IMAGE

sudo lxc stop test
sudo lxc copy test test2
sudo lxc start test
sudo lxc start test2
sleep 5

TEST_ADDR=$(sudo lxc exec test -- hostname -I | sed 's/ //')
TEST2_ADDR=$(sudo lxc exec test2 -- hostname -I | sed 's/ //')

echo "===== Benchmark: memcached rootful with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && sudo nerdctl run -p 4789:4789/udp --privileged --name memcached-server -d $MEMCACHED_IMAGE"
  NAME="test" exec_lxc sudo /home/ubuntu/bypass4netns/test/setup_vxlan.sh memcached-server $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && sudo nerdctl run -p 4789:4789/udp --privileged --name memcached-client -d --entrypoint '' $MEMTIRE_IMAGE /bin/sh -c 'sleep infinity'"
  NAME="test2" exec_lxc sudo /home/ubuntu/bypass4netns/test/setup_vxlan.sh memcached-client $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  sleep 5
  LOG_NAME="memcached-multinode-rootful.log"
  NAME="test2" exec_lxc sudo nerdctl exec memcached-client memtier_benchmark --host=$TEST1_VXLAN_ADDR --port=11211 --protocol=memcache_binary --json-out-file=/$LOG_NAME
  NAME="test2" exec_lxc sudo nerdctl exec memcached-client cat /$LOG_NAME > $LOG_NAME
  
  NAME="test" exec_lxc sudo nerdctl rm -f memcached-server
  NAME="test2" exec_lxc sudo nerdctl rm -f memcached-client
)

echo "===== Benchmark: memcached client(w/o bypass4netns) server(w/o bypass4netns) with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged --name memcached-server -d $MEMCACHED_IMAGE"
  NAME="test" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh memcached-server $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged --name memcached-client -d --entrypoint '' $MEMTIRE_IMAGE /bin/sh -c 'sleep infinity'"
  NAME="test2" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh memcached-client $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  sleep 5
  LOG_NAME="memcached-multinode-wo-b4ns.log"
  NAME="test2" exec_lxc nerdctl exec memcached-client memtier_benchmark --host=$TEST1_VXLAN_ADDR --port=11211 --protocol=memcache_binary --json-out-file=/$LOG_NAME
  NAME="test2" exec_lxc nerdctl exec memcached-client cat /$LOG_NAME > $LOG_NAME
  
  NAME="test" exec_lxc nerdctl rm -f memcached-server
  NAME="test2" exec_lxc nerdctl rm -f memcached-client
)

echo "===== Benchmark: memcached client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  NAME="test" exec_lxc systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$TEST_ADDR:2379 --advertise-client-urls http://$TEST_ADDR:2379
  NAME="test" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST_ADDR
  NAME="test2" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST2_ADDR
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --annotation nerdctl/bypass4netns=true -p 11212:11211 --name memcached-server -d $MEMCACHED_IMAGE"
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --annotation nerdctl/bypass4netns=true --name memcached-client -d --entrypoint '' $MEMTIRE_IMAGE /bin/sh -c 'sleep infinity'"
  SERVER_IP=$(NAME="test" exec_lxc nerdctl exec memcached-server hostname -i)
  sleep 5
  LOG_NAME="memcached-multinode-w-b4ns.log"
  NAME="test2" exec_lxc nerdctl exec memcached-client memtier_benchmark --host=$SERVER_IP --port=11211 --protocol=memcache_binary --json-out-file=/$LOG_NAME
  NAME="test2" exec_lxc nerdctl exec memcached-client cat /$LOG_NAME > $LOG_NAME

  NAME="test" exec_lxc nerdctl rm -f memcached-server
  NAME="test2" exec_lxc nerdctl rm -f memcached-client
)