#!/bin/bash
set -eu -o pipefail

cd $(dirname $0)

MEMCACHED_VERSION=1.6.22
MEMCACHED_IMAGE="memcached:${MEMCACHED_VERSION}"

MEMTIRE_VERSION=2.0.0
MEMTIRE_IMAGE="redislabs/memtier_benchmark:${MEMTIRE_VERSION}"

source ~/.profile

HOST_IP=$(HOST=$(hostname -I); for i in ${HOST[@]}; do echo $i | grep -q "192.168.6."; if [ $? -eq 0 ]; then echo $i; fi; done)
sudo nerdctl pull --quiet $MEMCACHED_IMAGE
sudo nerdctl pull --quiet $MEMTIRE_IMAGE
nerdctl pull --quiet $MEMCACHED_IMAGE
nerdctl pull --quiet $MEMTIRE_IMAGE

echo "===== Benchmark: memcached rootful via NetNS ====="
(
  set +e
  sudo nerdctl rm -f memcached-server
  sudo nerdctl rm -f memcached-client
  set -ex

  sudo nerdctl run -d --name memcached-server $MEMCACHED_IMAGE
  SERVER_IP=$(sudo nerdctl exec memcached-server hostname -i)
  LOG_NAME="memcached-rootful-direct.log"
  sudo nerdctl run --name memcached-client $MEMTIRE_IMAGE --host=$SERVER_IP --port=11211 --protocol=memcache_binary --json-out-file=/$LOG_NAME > /dev/null
  sudo nerdctl cp memcached-client:/$LOG_NAME ./$LOG_NAME

  sudo nerdctl rm -f memcached-server
  sudo nerdctl rm -f memcached-client
)

echo "===== Benchmark: memcached rootful via host ====="
(
  set +e
  sudo nerdctl rm -f memcached-server
  sudo nerdctl rm -f memcached-client
  set -ex

  sudo nerdctl run -d --name memcached-server -p 11212:11211 $MEMCACHED_IMAGE
  LOG_NAME="memcached-rootful-host.log"
  sudo nerdctl run --name memcached-client $MEMTIRE_IMAGE --host=$HOST_IP --port=11212 --protocol=memcache_binary --json-out-file=/$LOG_NAME > /dev/null
  sudo nerdctl cp memcached-client:/$LOG_NAME ./$LOG_NAME

  sudo nerdctl rm -f memcached-server
  sudo nerdctl rm -f memcached-client
)

echo "===== Benchmark: memcached client(w/o bypass4netns) server(w/o bypass4netns) via intermediate NetNS ====="
(
  set +e
  nerdctl rm -f memcached-server
  nerdctl rm -f memcached-client
  set -ex

  nerdctl run -d --name memcached-server $MEMCACHED_IMAGE
  SERVER_IP=$(nerdctl exec memcached-server hostname -i)
  LOG_NAME="memcached-wo-b4ns-direct.log"
  nerdctl run -d --name memcached-client --entrypoint '' $MEMTIRE_IMAGE /bin/sh -c "sleep infinity"
  nerdctl exec memcached-client memtier_benchmark --host=$SERVER_IP --port=11211 --protocol=memcache_binary --json-out-file=/$LOG_NAME > /dev/null
  nerdctl cp memcached-client:/$LOG_NAME ./$LOG_NAME

  nerdctl rm -f memcached-server
  nerdctl rm -f memcached-client
)

echo "===== Benchmark: memcached client(w/o bypass4netns) server(w/o bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f memcached-server
  nerdctl rm -f memcached-client
  set -ex

  nerdctl run -d --name memcached-server -p 11212:11211 $MEMCACHED_IMAGE
  LOG_NAME="memcached-wo-b4ns-host.log"
  nerdctl run -d --name memcached-client --entrypoint '' $MEMTIRE_IMAGE /bin/sh -c "sleep infinity"
  nerdctl exec memcached-client memtier_benchmark --host=$HOST_IP --port=11212 --protocol=memcache_binary --json-out-file=/$LOG_NAME > /dev/null
  nerdctl cp memcached-client:/$LOG_NAME ./$LOG_NAME

  nerdctl rm -f memcached-server
  nerdctl rm -f memcached-client
)

echo "===== Benchmark: memcached client(w/ bypass4netns) server(w/ bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f memcached-server
  nerdctl rm -f memcached-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit run-bypass4netnsd bypass4netnsd 

  nerdctl run --label nerdctl/bypass4netns=true -d --name memcached-server -p 11212:11211 $MEMCACHED_IMAGE
  LOG_NAME="memcached-w-b4ns.log"
  nerdctl run --label nerdctl/bypass4netns=true -d --name memcached-client --entrypoint '' $MEMTIRE_IMAGE /bin/sh -c "sleep infinity"
  nerdctl exec memcached-client memtier_benchmark --host=$HOST_IP --port=11212 --protocol=memcache_binary --json-out-file=/$LOG_NAME > /dev/null
  nerdctl cp memcached-client:/$LOG_NAME ./$LOG_NAME

  nerdctl rm -f memcached-server
  nerdctl rm -f memcached-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user reset-failed
)


echo "===== Benchmark: memcached client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  set +e
  nerdctl rm -f memcached-server
  nerdctl rm -f memcached-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$HOST_IP:2379 --advertise-client-urls http://$HOST_IP:2379
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$HOST_IP:2379 --multinode-host-address=$HOST_IP

  nerdctl run --label nerdctl/bypass4netns=true -d --name memcached-server -p 11212:11211 $MEMCACHED_IMAGE
  SERVER_IP=$(nerdctl exec memcached-server hostname -i)
  nerdctl run --label nerdctl/bypass4netns=true -d --name memcached-client --entrypoint '' $MEMTIRE_IMAGE /bin/sh -c "sleep infinity"
  nerdctl exec memcached-client memtier_benchmark --host=$SERVER_IP --port=11211 --protocol=memcache_binary

  nerdctl rm -f memcached-server
  nerdctl rm -f memcached-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
)
