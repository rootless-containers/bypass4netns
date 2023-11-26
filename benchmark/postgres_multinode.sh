#!/bin/bash

cd $(dirname $0)
. ../util.sh

set +e
NAME="test" exec_lxc nerdctl rm -f psql-server
sudo lxc rm -f test2

TEST1_VXLAN_MAC="02:42:c0:a8:00:1"
TEST1_VXLAN_ADDR="192.168.2.1"
TEST2_VXLAN_MAC="02:42:c0:a8:00:2"
TEST2_VXLAN_ADDR="192.168.2.2"
POSTGRES_VERSION=16.1
POSTGRES_IMAGE="postgres:$POSTGRES_VERSION"

set -eux -o pipefail

NAME="test" exec_lxc nerdctl pull --quiet $POSTGRES_IMAGE

sudo lxc stop test
sudo lxc copy test test2
sudo lxc start test
sudo lxc start test2
sleep 5

TEST_ADDR=$(sudo lxc exec test -- hostname -I | sed 's/ //')
TEST2_ADDR=$(sudo lxc exec test2 -- hostname -I | sed 's/ //')

echo "===== Benchmark: postgresql client(w/o bypass4netns) server(w/o bypass4netns) with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged --name psql-server -e POSTGRES_PASSWORD=pass -d $POSTGRES_IMAGE"
  NAME="test" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh psql-server $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged --name psql-client -e PGPASSWORD=pass -d $POSTGRES_IMAGE sleep infinity"
  NAME="test2" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh psql-client $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  sleep 5
  NAME="test2" exec_lxc nerdctl exec psql-client pgbench -h $TEST1_VXLAN_ADDR -U postgres -s 10 -i postgres
  NAME="test2" exec_lxc nerdctl exec psql-client pgbench -h $TEST1_VXLAN_ADDR -U postgres -s 10 -t 1000 postgres
  
  NAME="test" exec_lxc nerdctl rm -f psql-server
  NAME="test2" exec_lxc nerdctl rm -f psql-client
)

echo "===== Benchmark: postgresql client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  NAME="test" exec_lxc systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$TEST_ADDR:2379 --advertise-client-urls http://$TEST_ADDR:2379
  NAME="test" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST_ADDR
  NAME="test2" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST2_ADDR
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --label nerdctl/bypass4netns=true -d -p 15432:5432 --name psql-server -e POSTGRES_PASSWORD=pass $POSTGRES_IMAGE"
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --label nerdctl/bypass4netns=true -d --name psql-client -e PGPASSWORD=pass $POSTGRES_IMAGE sleep infinity"
  SERVER_IP=$(NAME="test" exec_lxc nerdctl exec psql-server hostname -i)
  sleep 5
  NAME="test2" exec_lxc nerdctl exec psql-client pgbench -h $SERVER_IP -U postgres -s 10 -i postgres
  NAME="test2" exec_lxc nerdctl exec psql-client pgbench -h $SERVER_IP -U postgres -s 10 -t 1000 postgres

  NAME="test" exec_lxc nerdctl rm -f psql-server
  NAME="test2" exec_lxc nerdctl rm -f psql-client
)