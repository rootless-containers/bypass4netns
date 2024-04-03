#!/bin/bash

cd $(dirname $0)
. ../../test/util.sh

set +e
NAME="test" exec_lxc sudo nerdctl rm -f mysql-server
NAME="test" exec_lxc nerdctl rm -f mysql-server
sudo lxc rm -f test2

TEST1_VXLAN_MAC="02:42:c0:a8:00:1"
TEST1_VXLAN_ADDR="192.168.2.1"
TEST2_VXLAN_MAC="02:42:c0:a8:00:2"
TEST2_VXLAN_ADDR="192.168.2.2"
MYSQL_VERSION=8.2.0
MYSQL_IMAGE="mysql:$MYSQL_VERSION"
BENCH_IMAGE="mysql-bench"

set -eux -o pipefail

# sometimes fail to pull images
# this is workaround
# https://github.com/containerd/nerdctl/issues/622
NAME="test" exec_lxc systemctl --user restart containerd
sleep 1
NAME="test" exec_lxc systemctl --user restart buildkit
sleep 3
NAME="test" exec_lxc systemctl --user status --no-pager containerd
NAME="test" exec_lxc systemctl --user status --no-pager buildkit
NAME="test" exec_lxc /bin/bash -c "cd /home/ubuntu/bypass4netns/benchmark/mysql && sudo nerdctl build -f ./Dockerfile -t $BENCH_IMAGE ."
NAME="test" exec_lxc /bin/bash -c "cd /home/ubuntu/bypass4netns/benchmark/mysql && nerdctl build -f ./Dockerfile -t $BENCH_IMAGE ."

NAME="test" exec_lxc sudo nerdctl pull --quiet $MYSQL_IMAGE
NAME="test" exec_lxc nerdctl pull --quiet $MYSQL_IMAGE

sudo lxc stop test
sudo lxc copy test test2
sudo lxc start test
sudo lxc start test2
sleep 5

TEST_ADDR=$(sudo lxc exec test -- hostname -I | sed 's/ //')
TEST2_ADDR=$(sudo lxc exec test2 -- hostname -I | sed 's/ //')

echo "===== Benchmark: mysql rootful with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && sudo nerdctl run -p 4789:4789/udp --privileged -d --name mysql-server -e MYSQL_ROOT_PASSWORD=pass -e MYSQL_DATABASE=bench $MYSQL_IMAGE"
  NAME="test" exec_lxc sudo /home/ubuntu/bypass4netns/test/setup_vxlan.sh mysql-server $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && sudo nerdctl run -p 4789:4789/udp --privileged -d --name mysql-client $BENCH_IMAGE sleep infinity"
  NAME="test2" exec_lxc sudo /home/ubuntu/bypass4netns/test/setup_vxlan.sh mysql-client $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  sleep 30
  NAME="test2" exec_lxc sudo nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$TEST1_VXLAN_ADDR --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_common prepare
  NAME="test2" exec_lxc sudo nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$TEST1_VXLAN_ADDR --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_read_write run > mysql-multinode-rootful.log
  
  NAME="test" exec_lxc sudo nerdctl rm -f mysql-server
  NAME="test2" exec_lxc sudo nerdctl rm -f mysql-client
)

echo "===== Benchmark: mysql client(w/o bypass4netns) server(w/o bypass4netns) with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged -d --name mysql-server -e MYSQL_ROOT_PASSWORD=pass -e MYSQL_DATABASE=bench $MYSQL_IMAGE"
  NAME="test" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh mysql-server $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged -d --name mysql-client $BENCH_IMAGE sleep infinity"
  NAME="test2" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh mysql-client $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  sleep 30
  NAME="test2" exec_lxc nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$TEST1_VXLAN_ADDR --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_common prepare
  NAME="test2" exec_lxc nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$TEST1_VXLAN_ADDR --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_read_write run > mysql-multinode-wo-b4ns.log
  
  NAME="test" exec_lxc nerdctl rm -f mysql-server
  NAME="test2" exec_lxc nerdctl rm -f mysql-client
)

echo "===== Benchmark: mysql client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  NAME="test" exec_lxc systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$TEST_ADDR:2379 --advertise-client-urls http://$TEST_ADDR:2379
  NAME="test" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST_ADDR
  NAME="test2" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST2_ADDR
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --annotation nerdctl/bypass4netns=true -d -p 13306:3306 --name mysql-server -e MYSQL_ROOT_PASSWORD=pass -e MYSQL_DATABASE=bench $MYSQL_IMAGE"
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --annotation nerdctl/bypass4netns=true -d --name mysql-client $BENCH_IMAGE sleep infinity"
  SERVER_IP=$(NAME="test" exec_lxc nerdctl inspect mysql-server | jq -r .[0].NetworkSettings.Networks.'"unknown-eth0"'.IPAddress)
  sleep 30
  NAME="test2" exec_lxc nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$SERVER_IP --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_common prepare
  NAME="test2" exec_lxc nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$SERVER_IP --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_read_write run > mysql-multinode-w-b4ns.log

  NAME="test" exec_lxc nerdctl rm -f mysql-server
  NAME="test2" exec_lxc nerdctl rm -f mysql-client
)