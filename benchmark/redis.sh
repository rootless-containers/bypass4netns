#!/bin/bash


set -eu -o pipefail

REDIS_VERSION=7.2.3
REDIS_IMAGE="redis:${REDIS_VERSION}"

source ~/.profile

nerdctl pull $REDIS_IMAGE

echo "===== Benchmark: redis client(w/o bypass4netns) server(w/o bypass4netns) via intermediate NetNS ====="
(
  set +e
  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
  set -ex

  nerdctl run -d --name redis-server "${REDIS_IMAGE}"
  nerdctl run -d --name redis-client "${REDIS_IMAGE}" sleep infinity
  SERVER_IP=$(nerdctl exec redis-server hostname -i)
  nerdctl exec redis-client redis-benchmark -q -h $SERVER_IP
  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
)

echo "===== Benchmark: redis client(w/o bypass4netns) server(w/o bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
  set -ex

  nerdctl run -d -p 6379:6379 --name redis-server "${REDIS_IMAGE}"
  nerdctl run -d --name redis-client "${REDIS_IMAGE}" sleep infinity
  SERVER_IP=$(hostname -I)
  nerdctl exec redis-client redis-benchmark -q -h $SERVER_IP
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


  # work around for https://github.com/naoki9911/bypass4netns/issues/1
  nerdctl run --label nerdctl/bypass4netns=true -d -p 6379:6379 --name redis-server --entrypoint '' "${REDIS_IMAGE}" redis-server
  nerdctl run --label nerdctl/bypass4netns=true -d --name redis-client "${REDIS_IMAGE}" sleep infinity
  SERVER_IP=$(hostname -I)
  nerdctl exec redis-client redis-benchmark -q -h $SERVER_IP

  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
  systemctl --user stop run-bypass4netnsd
)

echo "===== Benchmark: redis client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  set +e
  systemctl --user stop run-bypass4netnsd
  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
  systemctl --user reset-failed
  set -ex

  HOST_IP=$(hostname -I | sed 's/ //')
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$HOST_IP:2379 --multinode-host-address=$HOST_IP

  # work around for https://github.com/naoki9911/bypass4netns/issues/1
  nerdctl run --label nerdctl/bypass4netns=true -d -p 6379:6379 --name redis-server --entrypoint '' "${REDIS_IMAGE}" redis-server
  nerdctl run --label nerdctl/bypass4netns=true -d --name redis-client "${REDIS_IMAGE}" sleep infinity
  SERVER_IP=$(nerdctl exec redis-server hostname -i)
  # without this, benchmark is not performed.(race condition?)
  nerdctl exec redis-client /bin/sh -c "sleep 1 && redis-benchmark -q -h $SERVER_IP"

  nerdctl rm -f redis-server
  nerdctl rm -f redis-client
  systemctl --user stop run-bypass4netnsd
)