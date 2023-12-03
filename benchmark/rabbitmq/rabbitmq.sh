#!/bin/bash

RABBITMQ_VERSION=3.12.10
RABBITMQ_IMAGE="rabbitmq:$RABBITMQ_VERSION"

PERF_VERSION="2.20.0"
PERF_IMAGE="pivotalrabbitmq/perf-test:$PERF_VERSION"

source ~/.profile
cd $(dirname $0)

HOST_IP=$(HOST=$(hostname -I); for i in ${HOST[@]}; do echo $i | grep -q "192.168.6."; if [ $? -eq 0 ]; then echo $i; fi; done)
sudo nerdctl pull --quiet $RABBITMQ_IMAGE
sudo nerdctl pull --quiet $PERF_IMAGE
nerdctl pull --quiet $RABBITMQ_IMAGE
nerdctl pull --quiet $PERF_IMAGE

echo "===== Benchmark: rabbitmq rootful via NetNS ====="
(
  set +e
  sudo nerdctl rm -f rabbitmq-server
  set -ex

  sudo nerdctl run -d --name rabbitmq-server $RABBITMQ_IMAGE
  sleep 10
  SERVER_IP=$(sudo nerdctl exec rabbitmq-server hostname -i)
  LOG_NAME="rabbitmq-rootful-direct.log"
  sudo nerdctl run --name rabbitmq-client --rm $PERF_IMAGE --uri amqp://$SERVER_IP --producers 2 --consumers 2 --time 60 > $LOG_NAME

  sudo nerdctl rm -f rabbitmq-server
)

echo "===== Benchmark: rabbitmq rootful via host ====="
(
  set +e
  sudo nerdctl rm -f rabbitmq-server
  set -ex

  sudo nerdctl run -d --name rabbitmq-server -p 5673:5672 $RABBITMQ_IMAGE
  sleep 10
  SERVER_IP=$(sudo nerdctl exec rabbitmq-server hostname -i)
  LOG_NAME="rabbitmq-rootful-host.log"
  sudo nerdctl run --name rabbitmq-client --rm $PERF_IMAGE --uri amqp://$HOST_IP:5673 --producers 2 --consumers 2 --time 60 > $LOG_NAME

  sudo nerdctl rm -f rabbitmq-server
)

echo "===== Benchmark: rabbitmq client(w/o bypass4netns) server(w/o bypass4netns) via intermediate NetNS ====="
(
  set +e
  nerdctl rm -f rabbitmq-server
  set -ex

  nerdctl run -d --name rabbitmq-server $RABBITMQ_IMAGE
  sleep 10
  SERVER_IP=$(nerdctl exec rabbitmq-server hostname -i)
  LOG_NAME="rabbitmq-wo-b4ns-direct.log"
  nerdctl run --name rabbitmq-client --rm $PERF_IMAGE --uri amqp://$SERVER_IP --producers 2 --consumers 2 --time 60 > $LOG_NAME

  nerdctl rm -f rabbitmq-server
)

echo "===== Benchmark: rabbitmq client(w/o bypass4netns) server(w/o bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f rabbitmq-server
  set -ex

  nerdctl run -d --name rabbitmq-server -p 5673:5672 $RABBITMQ_IMAGE
  sleep 10
  SERVER_IP=$(nerdctl exec rabbitmq-server hostname -i)
  LOG_NAME="rabbitmq-wo-b4ns-host.log"
  nerdctl run --name rabbitmq-client --rm $PERF_IMAGE --uri amqp://$HOST_IP:5673 --producers 2 --consumers 2 --time 60 > $LOG_NAME

  nerdctl rm -f rabbitmq-server
)

echo "===== Benchmark: rabbitmq client(w/ bypass4netns) server(w/ bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f rabbitmq-server
  systemctl --user stop run-bypass4netnsd
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit run-bypass4netnsd bypass4netnsd 

  nerdctl run --label nerdctl/bypass4netns=true -d --name rabbitmq-server -p 5673:5672 $RABBITMQ_IMAGE
  sleep 10
  LOG_NAME="rabbitmq-w-b4ns.log"
  nerdctl run --label nerdctl/bypass4netns=true --name rabbitmq-client --rm $PERF_IMAGE --uri amqp://$HOST_IP:5673 --producers 2 --consumers 2 --time 60 > $LOG_NAME

  nerdctl rm -f rabbitmq-server
  systemctl --user stop run-bypass4netnsd
  systemctl --user reset-failed
)

echo "===== Benchmark: rabbitmq client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  set +e
  nerdctl rm -f rabbitmq-server
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$HOST_IP:2379 --advertise-client-urls http://$HOST_IP:2379
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$HOST_IP:2379 --multinode-host-address=$HOST_IP

  nerdctl run --label nerdctl/bypass4netns=true -d --name rabbitmq-server -p 5673:5672 $RABBITMQ_IMAGE
  sleep 10
  SERVER_IP=$(nerdctl exec rabbitmq-server hostname -i)
  nerdctl run --label nerdctl/bypass4netns=true --name rabbitmq-client --rm $PERF_IMAGE --uri amqp://$SERVER_IP --producers 2 --consumers 2 --time 60

  nerdctl rm -f rabbitmq-server
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
)