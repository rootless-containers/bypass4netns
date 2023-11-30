#!/bin/bash
set -eu -o pipefail

cd $(dirname $0)

REDIS_VERSION=7.2.3
REDIS_IMAGE="redis:${REDIS_VERSION}"

source ~/.profile

nerdctl pull --quiet $REDIS_IMAGE

echo "===== Benchmark: redis client(w/o bypass4netns) server(w/o bypass4netns) via intermediate NetNS ====="
(
  set +e
  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
  set -ex

  nerdctl run -d --name redis-server "${REDIS_IMAGE}"
  nerdctl run -d --name redis-client "${REDIS_IMAGE}" sleep infinity
  SERVER_IP=$(nerdctl exec redis-server hostname -i)
  nerdctl exec redis-client redis-benchmark -q -h $SERVER_IP --csv > redis-wo-b4ns-direct.log
  cat redis-wo-b4ns-direct.log

  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
)

echo "===== Benchmark: redis client(w/o bypass4netns) server(w/o bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
  set -ex

  nerdctl run -d -p 6380:6379 --name redis-server "${REDIS_IMAGE}"
  nerdctl run -d --name redis-client "${REDIS_IMAGE}" sleep infinity
  SERVER_IP=$(hostname -I | awk '{print $1}')
  nerdctl exec redis-client redis-benchmark -q -h $SERVER_IP -p 6380 --csv > redis-wo-b4ns-host.log
  cat redis-wo-b4ns-host.log

  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
)

echo "===== Benchmark: redis client(w/ bypass4netns) server(w/ bypass4netns) via host ====="
(
  set +e
  systemctl --user stop run-bypass4netnsd
  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit run-bypass4netnsd bypass4netnsd 

  nerdctl run --label nerdctl/bypass4netns=true -d -p 6380:6379 --name redis-server $REDIS_IMAGE
  nerdctl run --label nerdctl/bypass4netns=true -d --name redis-client $REDIS_IMAGE sleep infinity
  SERVER_IP=$(hostname -I | awk '{print $1}')
  nerdctl exec redis-client redis-benchmark -q -h $SERVER_IP -p 6380 --csv > redis-w-b4ns.log
  cat redis-w-b4ns.log

  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
  systemctl --user stop run-bypass4netnsd
)

echo "===== Benchmark: redis client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  set +e
  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
  set -ex

  HOST_IP=$(hostname -I | awk '{print $1}')
  systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://${HOST_IP}:2379 --advertise-client-urls http://${HOST_IP}:2379
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$HOST_IP:2379 --multinode-host-address=$HOST_IP

  nerdctl run --label nerdctl/bypass4netns=true -d -p 6380:6379 --name redis-server $REDIS_IMAGE
  nerdctl run --label nerdctl/bypass4netns=true -d --name redis-client $REDIS_IMAGE sleep infinity
  SERVER_IP=$(nerdctl exec redis-server hostname -i)
  # without 'sleep 1', benchmark is not performed.(race condition?)
  nerdctl exec redis-client /bin/sh -c "sleep 1 && redis-benchmark -q -h $SERVER_IP -p 6379 --csv" 

  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
)
