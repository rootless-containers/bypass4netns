#!/bin/bash

cd $(dirname $0)
. ../../util.sh

set +e
NAME="test" exec_lxc sudo nerdctl rm -f etcd-server
NAME="test" exec_lxc nerdctl rm -f etcd-server
sudo lxc rm -f test2

TEST1_VXLAN_MAC="02:42:c0:a8:00:1"
TEST1_VXLAN_ADDR="192.168.2.1"
TEST2_VXLAN_MAC="02:42:c0:a8:00:2"
TEST2_VXLAN_ADDR="192.168.2.2"
ETCD_VERSION="v3.3.25"
ETCD_IMAGE="quay.io/coreos/etcd:${ETCD_VERSION}"
BENCH_IMAGE="etcd-bench"

set -eux -o pipefail

NAME="test" exec_lxc sudo nerdctl pull --quiet $ETCD_IMAGE
NAME="test" exec_lxc nerdctl pull --quiet $ETCD_IMAGE
NAME="test" exec_lxc systemctl --user restart containerd
sleep 1
NAME="test" exec_lxc systemctl --user restart buildkit
sleep 3
NAME="test" exec_lxc systemctl --user status --no-pager containerd
NAME="test" exec_lxc systemctl --user status --no-pager buildkit
NAME="test" exec_lxc /bin/bash -c "cd /home/ubuntu/bypass4netns/benchmark/etcd && sudo nerdctl build -f ./Dockerfile -t $BENCH_IMAGE ."
NAME="test" exec_lxc /bin/bash -c "cd /home/ubuntu/bypass4netns/benchmark/etcd && nerdctl build -f ./Dockerfile -t $BENCH_IMAGE ."

sudo lxc stop test
sudo lxc copy test test2
sudo lxc start test
sudo lxc start test2
sleep 5

TEST_ADDR=$(sudo lxc exec test -- hostname -I | sed 's/ //')
TEST2_ADDR=$(sudo lxc exec test2 -- hostname -I | sed 's/ //')

echo "===== Benchmark: etcd rootful with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && sudo nerdctl run -p 4789:4789/udp --privileged --name etcd-server -d $ETCD_IMAGE /bin/sh -c 'sleep infinity'"
  NAME="test" exec_lxc sudo /home/ubuntu/bypass4netns/test/setup_vxlan.sh etcd-server $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test" exec_lxc systemd-run --user --unit etcd-server sudo nerdctl exec etcd-server /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 --advertise-client-urls http://$TEST1_VXLAN_ADDR:2379
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && sudo nerdctl run -p 4789:4789/udp --privileged --name etcd-client -d $BENCH_IMAGE /bin/sh -c 'sleep infinity'"
  NAME="test2" exec_lxc sudo /home/ubuntu/bypass4netns/test/setup_vxlan.sh etcd-client $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  sleep 5
  LOG_NAME="etcd-multinode-rootful.log"
  NAME="test2" exec_lxc sudo nerdctl exec etcd-client /bench put --key-size=8 --val-size=256 --conns=10 --clients=10 --total=100000 --endpoints $TEST1_VXLAN_ADDR:2379 > $LOG_NAME
  
  NAME="test" exec_lxc sudo nerdctl rm -f etcd-server
  NAME="test" exec_lxc systemctl --user stop etcd-server
  NAME="test" exec_lxc systemctl --user reset-failed
  NAME="test2" exec_lxc sudo nerdctl rm -f etcd-client
)

echo "===== Benchmark: etcd client(w/o bypass4netns) server(w/o bypass4netns) with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged --name etcd-server -d $ETCD_IMAGE /bin/sh -c 'sleep infinity'"
  NAME="test" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh etcd-server $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test" exec_lxc systemd-run --user --unit etcd-server nerdctl exec etcd-server /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 --advertise-client-urls http://$TEST1_VXLAN_ADDR:2379
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged --name etcd-client -d $BENCH_IMAGE /bin/sh -c 'sleep infinity'"
  NAME="test2" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh etcd-client $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  sleep 5
  LOG_NAME="etcd-multinode-wo-b4ns.log"
  NAME="test2" exec_lxc nerdctl exec etcd-client /bench put --key-size=8 --val-size=256 --conns=10 --clients=10 --total=100000 --endpoints $TEST1_VXLAN_ADDR:2379 > $LOG_NAME
  
  NAME="test" exec_lxc nerdctl rm -f etcd-server
  NAME="test" exec_lxc systemctl --user stop etcd-server
  NAME="test" exec_lxc systemctl --user reset-failed
  NAME="test2" exec_lxc nerdctl rm -f etcd-client
)

echo "===== Benchmark: etcd client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  NAME="test" exec_lxc systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$TEST_ADDR:2379 --advertise-client-urls http://$TEST_ADDR:2379
  NAME="test" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST_ADDR
  NAME="test2" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST2_ADDR
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --label nerdctl/bypass4netns=true -p 12379:2379 --name etcd-server -d $ETCD_IMAGE /bin/sh -c 'sleep infinity'"
  SERVER_IP=$(NAME="test" exec_lxc nerdctl exec etcd-server hostname -i)
  NAME="test" exec_lxc systemd-run --user --unit etcd-server nerdctl exec etcd-server /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 --advertise-client-urls http://$SERVER_IP:2379
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --label nerdctl/bypass4netns=true --name etcd-client -d $BENCH_IMAGE /bin/sh -c 'sleep infinity'"
  sleep 5
  LOG_NAME="etcd-multinode-w-b4ns.log"
  NAME="test2" exec_lxc nerdctl exec etcd-client /bench put --key-size=8 --val-size=256 --conns=10 --clients=10 --total=100000 --endpoints $SERVER_IP:2379 > $LOG_NAME

  NAME="test" exec_lxc nerdctl rm -f etcd-server
  NAME="test2" exec_lxc nerdctl rm -f etcd-client
)