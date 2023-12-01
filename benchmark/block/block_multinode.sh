#!/bin/bash

cd $(dirname $0)
. ../../util.sh

set +e
NAME="test" exec_lxc sudo nerdctl rm -f block-server
NAME="test" exec_lxc nerdctl rm -f block-server
sudo lxc rm -f test2

TEST1_VXLAN_MAC="02:42:c0:a8:00:1"
TEST1_VXLAN_ADDR="192.168.2.1"
TEST2_VXLAN_MAC="02:42:c0:a8:00:2"
TEST2_VXLAN_ADDR="192.168.2.2"
IMAGE_NAME="block"
COUNT="10"
BLOCK_SIZES=('1k' '32k' '128k' '512k' '1m' '32m' '128m' '512m' '1g')

set -eux -o pipefail

NAME="test" exec_lxc systemctl --user restart containerd
sleep 1
NAME="test" exec_lxc systemctl --user restart buildkit
sleep 3
NAME="test" exec_lxc systemctl --user status --no-pager containerd
NAME="test" exec_lxc systemctl --user status --no-pager buildkit
NAME="test" exec_lxc /bin/bash -c "cd /home/ubuntu/bypass4netns/benchmark/block && sudo nerdctl build -f ./Dockerfile -t $IMAGE_NAME ."
NAME="test" exec_lxc /bin/bash -c "cd /home/ubuntu/bypass4netns/benchmark/block && nerdctl build -f ./Dockerfile -t $IMAGE_NAME ."

sudo lxc stop test
sudo lxc copy test test2
sudo lxc start test
sudo lxc start test2
sleep 5

TEST_ADDR=$(sudo lxc exec test -- hostname -I | sed 's/ //')
TEST2_ADDR=$(sudo lxc exec test2 -- hostname -I | sed 's/ //')

NAME="test" exec_lxc /home/ubuntu/bypass4netns/benchmark/block/gen_blocks.sh

echo "===== Benchmark: block rootful with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && sudo nerdctl run -p 4789:4789/udp --privileged -d --name block-server -v /home/ubuntu/bypass4netns/benchmark/block:/var/www/html:ro $IMAGE_NAME nginx -g \"daemon off;\""
  NAME="test" exec_lxc sudo /home/ubuntu/bypass4netns/test/setup_vxlan.sh block-server $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && sudo nerdctl run -p 4789:4789/udp --privileged -d --name block-client $IMAGE_NAME sleep infinity"
  NAME="test2" exec_lxc sudo /home/ubuntu/bypass4netns/test/setup_vxlan.sh block-client $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  LOG_NAME="block-multinode-rootful.log"
  rm -f $LOG_NAME
  for BLOCK_SIZE in ${BLOCK_SIZES[@]}
  do
    NAME="test2" exec_lxc /bin/bash -c "sudo nerdctl exec block-client /bench -count $COUNT -thread-num 1 -url http://$TEST1_VXLAN_ADDR/blk-$BLOCK_SIZE" >> $LOG_NAME
  done
  
  NAME="test" exec_lxc sudo nerdctl rm -f block-server
  NAME="test2" exec_lxc sudo nerdctl rm -f block-client
)

echo "===== Benchmark: block client(w/o bypass4netns) server(w/o bypass4netns) with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged -d --name block-server -v /home/ubuntu/bypass4netns/benchmark/block:/var/www/html:ro $IMAGE_NAME nginx -g \"daemon off;\""
  NAME="test" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh block-server $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged -d --name block-client $IMAGE_NAME sleep infinity"
  NAME="test2" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh block-client $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  LOG_NAME="block-multinode-wo-b4ns.log"
  rm -f $LOG_NAME
  for BLOCK_SIZE in ${BLOCK_SIZES[@]}
  do
    NAME="test2" exec_lxc /bin/bash -c "nerdctl exec block-client /bench -count $COUNT -thread-num 1 -url http://$TEST1_VXLAN_ADDR/blk-$BLOCK_SIZE" >> $LOG_NAME
  done
  
  NAME="test" exec_lxc nerdctl rm -f block-server
  NAME="test2" exec_lxc nerdctl rm -f block-client
)

echo "===== Benchmark: block client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  NAME="test" exec_lxc systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$TEST_ADDR:2379 --advertise-client-urls http://$TEST_ADDR:2379
  NAME="test" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST_ADDR
  NAME="test2" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST2_ADDR
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --label nerdctl/bypass4netns=true -p 8080:80 -d --name block-server -v /home/ubuntu/bypass4netns/benchmark/block:/var/www/html:ro $IMAGE_NAME nginx -g \"daemon off;\""
  SERVER_IP=$(NAME="test" exec_lxc nerdctl exec block-server hostname -i)
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --label nerdctl/bypass4netns=true -d --name block-client $IMAGE_NAME sleep infinity"
  LOG_NAME="block-multinode-w-b4ns.log"
  rm -f $LOG_NAME
  for BLOCK_SIZE in ${BLOCK_SIZES[@]}
  do
    NAME="test2" exec_lxc /bin/bash -c "nerdctl exec block-client /bench -count $COUNT -thread-num 1 -url http://$SERVER_IP/blk-$BLOCK_SIZE" >> $LOG_NAME
  done
  
  NAME="test" exec_lxc nerdctl rm -f block-server
  NAME="test2" exec_lxc nerdctl rm -f block-client
)
