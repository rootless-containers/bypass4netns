#!/bin/bash

cd $(dirname $0)
. ../util.sh

set +e
NAME="test" exec_lxc nerdctl rm -f redis-server
sudo lxc rm -f test2

TEST1_VXLAN_MAC="02:42:c0:a8:00:1"
TEST1_VXLAN_ADDR="192.168.2.1"
TEST2_VXLAN_MAC="02:42:c0:a8:00:2"
TEST2_VXLAN_ADDR="192.168.2.2"
REDIS_VERSION=7.2.3
REDIS_IMAGE="redis:${REDIS_VERSION}"

set -eux -o pipefail

NAME="test" exec_lxc nerdctl pull --quiet $REDIS_IMAGE

sudo lxc stop test
sudo lxc copy test test2
sudo lxc start test
sudo lxc start test2
sleep 5

TEST_ADDR=$(sudo lxc exec test -- hostname -I | sed 's/ //')
TEST2_ADDR=$(sudo lxc exec test2 -- hostname -I | sed 's/ //')

echo "===== Benchmark: redis client(w/o bypass4netns) server(w/o bypass4netns) with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged --name redis-server -d $REDIS_IMAGE"
  NAME="test" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh redis-server $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged --name redis-client -d $REDIS_IMAGE  sleep infinity"
  NAME="test2" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh redis-client $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  NAME="test2" exec_lxc nerdctl exec redis-client redis-benchmark -q -h $TEST1_VXLAN_ADDR
  
  NAME="test" exec_lxc nerdctl rm -f redis-server
  NAME="test2" exec_lxc nerdctl rm -f redis-client
)


echo "===== Benchmark: redis client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  NAME="test" exec_lxc systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$TEST_ADDR:2379 --advertise-client-urls http://$TEST_ADDR:2379
  NAME="test" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST_ADDR
  NAME="test2" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST2_ADDR
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --label nerdctl/bypass4netns=true -d -p 6379:6379 --name redis-server --entrypoint '' $REDIS_IMAGE redis-server"
  SERVER_IP=$(NAME="test" exec_lxc nerdctl exec redis-server hostname -i)
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --label nerdctl/bypass4netns=true -d --name redis-client $REDIS_IMAGE sleep infinity"
  NAME="test2" exec_lxc nerdctl exec redis-client /bin/sh -c "sleep 1 && redis-benchmark -q -h $SERVER_IP"

  NAME="test" exec_lxc nerdctl rm -f redis-server
  NAME="test2" exec_lxc nerdctl rm -f redis-client
)