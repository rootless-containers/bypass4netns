#!/bin/bash

cd $(dirname $0)
. ./util.sh

set +e
NAME="test" exec_lxc nerdctl rm -f vxlan
sudo lxc rm -f test2

TEST1_VXLAN_MAC="02:42:c0:a8:00:1"
TEST1_VXLAN_ADDR="192.168.2.1"
TEST2_VXLAN_MAC="02:42:c0:a8:00:2"
TEST2_VXLAN_ADDR="192.168.2.2"
ALPINE_IMAGE="public.ecr.aws/docker/library/alpine:3.16"

set -eux -o pipefail

sudo lxc stop test
sudo lxc copy test test2
sudo lxc start test
sudo lxc start test2
sleep 5

TEST_ADDR=$(sudo lxc exec test -- hostname -I | sed 's/ //')
TEST2_ADDR=$(sudo lxc exec test2 -- hostname -I | sed 's/ //')

echo "===== Benchmark: iperf3 client(w/o bypass4netns) server(w/o bypass4netns) with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged --name vxlan -d $ALPINE_IMAGE sleep infinity"
  NAME="test" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh vxlan $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test" exec_lxc nerdctl exec vxlan apk add --no-cache iperf3
  NAME="test" exec_lxc systemd-run --user --unit run-test-iperf3 nerdctl exec vxlan iperf3 -s
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged --name vxlan -d $ALPINE_IMAGE sleep infinity"
  NAME="test2" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh vxlan $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  NAME="test2" exec_lxc nerdctl exec vxlan apk add --no-cache iperf3
  NAME="test2" exec_lxc nerdctl exec vxlan iperf3 -c $TEST1_VXLAN_ADDR

  NAME="test" exec_lxc nerdctl rm -f vxlan
  NAME="test" exec_lxc systemctl --user reset-failed
  NAME="test2" exec_lxc nerdctl rm -f vxlan
)

echo "===== Benchmark: iperf3 client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  NAME="test" exec_lxc systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$TEST_ADDR:2379 --advertise-client-urls http://$TEST_ADDR:2379
  NAME="test" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST_ADDR
  NAME="test2" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST2_ADDR
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --annotation nerdctl/bypass4netns=true -d -p 8080:5201 --name vxlan $ALPINE_IMAGE sleep infinity"
  NAME="test" exec_lxc nerdctl exec vxlan apk add --no-cache iperf3
  NAME="test" exec_lxc systemd-run --user --unit run-test-iperf3 nerdctl exec vxlan iperf3 -s
  SERVER_IP=$(NAME="test" exec_lxc nerdctl exec vxlan hostname -i)
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --annotation nerdctl/bypass4netns=true -d --name vxlan $ALPINE_IMAGE sleep infinity"
  NAME="test2" exec_lxc nerdctl exec vxlan apk add --no-cache iperf3
  NAME="test2" exec_lxc nerdctl exec vxlan iperf3 -c $SERVER_IP

  NAME="test" exec_lxc nerdctl rm -f vxlan
  NAME="test2" exec_lxc nerdctl rm -f vxlan
)
