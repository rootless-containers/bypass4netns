#!/bin/bash

cd $(dirname $0)
. ../../test/util.sh

set +e
NAME="test" exec_lxc sudo nerdctl rm -f rabbitmq-server
NAME="test" exec_lxc nerdctl rm -f rabbitmq-server
sudo lxc rm -f test2

TEST1_VXLAN_MAC="02:42:c0:a8:00:1"
TEST1_VXLAN_ADDR="192.168.2.1"
TEST2_VXLAN_MAC="02:42:c0:a8:00:2"
TEST2_VXLAN_ADDR="192.168.2.2"

RABBITMQ_VERSION=3.12.10
RABBITMQ_IMAGE="rabbitmq:$RABBITMQ_VERSION"

PERF_VERSION="2.20.0"
PERF_IMAGE="pivotalrabbitmq/perf-test:$PERF_VERSION"

set -eux -o pipefail

NAME="test" exec_lxc sudo nerdctl pull --quiet $RABBITMQ_IMAGE
NAME="test" exec_lxc sudo nerdctl pull --quiet $PERF_IMAGE
NAME="test" exec_lxc nerdctl pull --quiet $RABBITMQ_IMAGE
NAME="test" exec_lxc nerdctl pull --quiet $PERF_IMAGE

sudo lxc stop test
sudo lxc copy test test2
sudo lxc start test
sudo lxc start test2
sleep 5

TEST_ADDR=$(sudo lxc exec test -- hostname -I | sed 's/ //')
TEST2_ADDR=$(sudo lxc exec test2 -- hostname -I | sed 's/ //')

echo "===== Benchmark: rabbitmq rootful with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && sudo nerdctl run -p 4789:4789/udp --privileged --name rabbitmq-server -d $RABBITMQ_IMAGE"
  NAME="test" exec_lxc sudo /home/ubuntu/bypass4netns/test/setup_vxlan.sh rabbitmq-server $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && sudo nerdctl run -p 4789:4789/udp --privileged --name rabbitmq-client -d --entrypoint '' $PERF_IMAGE /bin/sh -c 'sleep infinity'"
  NAME="test2" exec_lxc sudo /home/ubuntu/bypass4netns/test/setup_vxlan.sh rabbitmq-client $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  sleep 5
  LOG_NAME="rabbitmq-multinode-rootful.log"
  NAME="test2" exec_lxc sudo nerdctl exec rabbitmq-client java -jar /perf_test/perf-test.jar --uri amqp://$TEST1_VXLAN_ADDR --producers 2 --consumers 2 --time 60 > $LOG_NAME
  
  NAME="test" exec_lxc sudo nerdctl rm -f rabbitmq-server
  NAME="test2" exec_lxc sudo nerdctl rm -f rabbitmq-client
)

echo "===== Benchmark: rabbitmq client(w/o bypass4netns) server(w/o bypass4netns) with multinode via VXLAN ====="
(
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged --name rabbitmq-server -d $RABBITMQ_IMAGE"
  NAME="test" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh rabbitmq-server $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR $TEST2_ADDR $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run -p 4789:4789/udp --privileged --name rabbitmq-client -d --entrypoint '' $PERF_IMAGE /bin/sh -c 'sleep infinity'"
  NAME="test2" exec_lxc /home/ubuntu/bypass4netns/test/setup_vxlan.sh rabbitmq-client $TEST2_VXLAN_MAC $TEST2_VXLAN_ADDR $TEST_ADDR $TEST1_VXLAN_MAC $TEST1_VXLAN_ADDR
  sleep 5
  LOG_NAME="rabbitmq-multinode-wo-b4ns.log"
  NAME="test2" exec_lxc nerdctl exec rabbitmq-client java -jar /perf_test/perf-test.jar --uri amqp://$TEST1_VXLAN_ADDR --producers 2 --consumers 2 --time 60 > $LOG_NAME
  
  NAME="test" exec_lxc nerdctl rm -f rabbitmq-server
  NAME="test2" exec_lxc nerdctl rm -f rabbitmq-client
)

echo "===== Benchmark: rabbitmq client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  NAME="test" exec_lxc systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$TEST_ADDR:2379 --advertise-client-urls http://$TEST_ADDR:2379
  NAME="test" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST_ADDR
  NAME="test2" exec_lxc systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$TEST_ADDR:2379 --multinode-host-address=$TEST2_ADDR
  NAME="test" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --label nerdctl/bypass4netns=true -p 5673:5672 --name rabbitmq-server -d $RABBITMQ_IMAGE"
  NAME="test2" exec_lxc /bin/bash -c "sleep 3 && nerdctl run --label nerdctl/bypass4netns=true --name rabbitmq-client -d --entrypoint '' $PERF_IMAGE /bin/sh -c 'sleep infinity'"
  sleep 5
  SERVER_IP=$(NAME="test" exec_lxc nerdctl exec rabbitmq-server hostname -i)
  LOG_NAME="rabbitmq-multinode-w-b4ns.log"
  NAME="test2" exec_lxc nerdctl exec rabbitmq-client java -jar /perf_test/perf-test.jar --uri amqp://$SERVER_IP --producers 2 --consumers 2 --time 60 > $LOG_NAME
  
  NAME="test" exec_lxc nerdctl rm -f rabbitmq-server
  NAME="test2" exec_lxc nerdctl rm -f rabbitmq-client
)
