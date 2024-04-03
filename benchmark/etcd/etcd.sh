#!/bin/bash
set -eu -o pipefail

cd $(dirname $0)


ETCD_VERSION="v3.3.25"
ETCD_IMAGE="quay.io/coreos/etcd:${ETCD_VERSION}"
BENCH_IMAGE="etcd-bench"

source ~/.profile
. ../param.bash

# sometimes fail to pull images
# this is workaround
# https://github.com/containerd/nerdctl/issues/622
systemctl --user restart containerd
sleep 1
systemctl --user restart buildkit
sleep 3
systemctl --user status --no-pager containerd
systemctl --user status --no-pager buildkit
sudo nerdctl build -f ./Dockerfile -t $BENCH_IMAGE .
nerdctl build -f ./Dockerfile -t $BENCH_IMAGE .

sudo nerdctl pull --quiet $ETCD_IMAGE
nerdctl pull --quiet $ETCD_IMAGE

echo "===== Benchmark: etcd rootful via NetNS ====="
(
  set +e
  sudo nerdctl rm -f etcd-server
  sudo nerdctl rm -f etcd-client
  systemctl --user stop etcd-server
  systemctl --user reset-failed
  set -ex

  sudo nerdctl run -d --name etcd-server $ETCD_IMAGE /bin/sh -c "sleep infinity"
  SERVER_IP=$(sudo nerdctl exec etcd-server hostname -i)
  systemd-run --user --unit etcd-server sudo nerdctl exec etcd-server /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 --advertise-client-urls http://$SERVER_IP:2379
  sleep 5
  LOG_NAME="etcd-rootful-direct.log"
  sudo nerdctl run --rm $BENCH_IMAGE /bench put --key-size=8 --val-size=256 --conns=10 --clients=10 --total=100000 --endpoints $SERVER_IP:2379 > $LOG_NAME

  sudo nerdctl rm -f etcd-server
  sudo nerdctl rm -f etcd-client
  systemctl --user stop etcd-server
  systemctl --user reset-failed
)

echo "===== Benchmark: etcd rootful via host ====="
(
  set +e
  sudo nerdctl rm -f etcd-server
  sudo nerdctl rm -f etcd-client
  set -ex

  sudo nerdctl run -d --name etcd-server -p 12379:2379 $ETCD_IMAGE /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 --advertise-client-urls http://$HOST_IP:2379
  sleep 5
  LOG_NAME="etcd-rootful-host.log"
  sudo nerdctl run --rm $BENCH_IMAGE /bench put --key-size=8 --val-size=256 --conns=10 --clients=10 --total=100000 --endpoints $HOST_IP:12379 > $LOG_NAME

  sudo nerdctl rm -f etcd-server
  sudo nerdctl rm -f etcd-client
)

echo "===== Benchmark: etcd client(w/o bypass4netns) server(w/o bypass4netns) via intermediate NetNS ====="
(
  set +e
  nerdctl rm -f etcd-server
  nerdctl rm -f etcd-client
  systemctl --user stop etcd-server
  systemctl --user reset-failed
  set -ex

  nerdctl run -d --name etcd-server $ETCD_IMAGE /bin/sh -c "sleep infinity"
  SERVER_IP=$(nerdctl exec etcd-server hostname -i)
  systemd-run --user --unit etcd-server nerdctl exec etcd-server /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 --advertise-client-urls http://$SERVER_IP:2379
  sleep 5
  LOG_NAME="etcd-wo-b4ns-direct.log"
  nerdctl run --rm $BENCH_IMAGE /bench put --key-size=8 --val-size=256 --conns=10 --clients=10 --total=100000 --endpoints $SERVER_IP:2379 > $LOG_NAME

  nerdctl rm -f etcd-server
  nerdctl rm -f etcd-client
  systemctl --user stop etcd-server
  systemctl --user reset-failed
)

echo "===== Benchmark: etcd client(w/o bypass4netns) server(w/o bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f etcd-server
  nerdctl rm -f etcd-client
  set -ex

  nerdctl run -d --name etcd-server -p 12379:2379 $ETCD_IMAGE /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 --advertise-client-urls http://$HOST_IP:2379
  sleep 5
  LOG_NAME="etcd-wo-b4ns-host.log"
  nerdctl run --rm $BENCH_IMAGE /bench put --key-size=8 --val-size=256 --conns=10 --clients=10 --total=100000 --endpoints $HOST_IP:12379 > $LOG_NAME

  nerdctl rm -f etcd-server
  nerdctl rm -f etcd-client
)

echo "===== Benchmark: etcd client(w/ bypass4netns) server(w/ bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f etcd-server
  nerdctl rm -f etcd-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user reset-failed
  set -ex
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd 

  nerdctl run --annotation nerdctl/bypass4netns=true -d --name etcd-server -p 12379:2379 $ETCD_IMAGE /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 --advertise-client-urls http://$HOST_IP:2379
  sleep 5
  LOG_NAME="etcd-w-b4ns.log"
  nerdctl run --annotation nerdctl/bypass4netns=true --rm $BENCH_IMAGE /bench put --key-size=8 --val-size=256 --conns=10 --clients=10 --total=100000 --endpoints $HOST_IP:12379 > $LOG_NAME

  nerdctl rm -f etcd-server
  nerdctl rm -f etcd-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user reset-failed
)

echo "===== Benchmark: etcd client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  set +e
  nerdctl rm -f etcd-server
  nerdctl rm -f etcd-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$HOST_IP:2379 --advertise-client-urls http://$HOST_IP:2379
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$HOST_IP:2379 --multinode-host-address=$HOST_IP

  nerdctl run --annotation nerdctl/bypass4netns=true -d --name etcd-server -p 12379:2379 $ETCD_IMAGE /bin/sh -c "sleep infinity"
  sleep 5
  SERVER_IP=$(nerdctl exec etcd-server hostname -i)
  systemd-run --user --unit etcd-server nerdctl exec etcd-server /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 --advertise-client-urls http://$SERVER_IP:2379
  nerdctl run --annotation nerdctl/bypass4netns=true --rm $BENCH_IMAGE /bench put --key-size=8 --val-size=256 --conns=10 --clients=10 --total=100000 --endpoints $SERVER_IP:2379

  nerdctl rm -f etcd-server
  nerdctl rm -f etcd-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
)
