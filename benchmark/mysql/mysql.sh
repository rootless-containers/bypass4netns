#!/bin/bash

set -eu -o pipefail

MYSQL_VERSION=8.2.0
MYSQL_IMAGE="mysql:$MYSQL_VERSION"
BENCH_IMAGE="mysql-bench"

source ~/.profile
cd $(dirname $0)
. ../../util.sh
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

sudo nerdctl pull --quiet $MYSQL_IMAGE
nerdctl pull --quiet $MYSQL_IMAGE

echo "===== Benchmark: mysql rootful via NetNS ====="
(
  set +e
  sudo nerdctl rm -f mysql-server
  sudo nerdctl rm -f mysql-client
  set -ex

  sudo nerdctl run -d --name mysql-server -e MYSQL_ROOT_PASSWORD=pass -e MYSQL_DATABASE=bench $MYSQL_IMAGE
  sudo nerdctl run -d --name mysql-client $BENCH_IMAGE sleep infinity
  SERVER_IP=$(sudo nerdctl inspect mysql-server | jq -r .[0].NetworkSettings.Networks.'"unknown-eth0"'.IPAddress)
  sleep 30
  sudo nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$SERVER_IP --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_common prepare
  sudo nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$SERVER_IP --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_read_write run > mysql-rootful-direct.log

  sudo nerdctl rm -f mysql-server
  sudo nerdctl rm -f mysql-client
)

echo "===== Benchmark: mysql rootful via host ====="
(
  set +e
  sudo nerdctl rm -f mysql-server
  sudo nerdctl rm -f mysql-client
  set -ex

  sudo nerdctl run -d -p 13306:3306 --name mysql-server -e MYSQL_ROOT_PASSWORD=pass -e MYSQL_DATABASE=bench $MYSQL_IMAGE
  sudo nerdctl run -d --name mysql-client $BENCH_IMAGE sleep infinity
  sleep 30
  sudo nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$HOST_IP --mysql-port=13306 --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_common prepare
  sudo nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$HOST_IP --mysql-port=13306 --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_read_write run > mysql-rootful-host.log

  sudo nerdctl rm -f mysql-server
  sudo nerdctl rm -f mysql-client
)

echo "===== Benchmark: mysql client(w/o bypass4netns) server(w/o bypass4netns) via intermediate NetNS ====="
(
  set +e
  nerdctl rm -f mysql-server
  nerdctl rm -f mysql-client
  set -ex

  nerdctl run -d --name mysql-server -e MYSQL_ROOT_PASSWORD=pass -e MYSQL_DATABASE=bench $MYSQL_IMAGE
  nerdctl run -d --name mysql-client $BENCH_IMAGE sleep infinity
  SERVER_IP=$(nerdctl inspect mysql-server | jq -r .[0].NetworkSettings.Networks.'"unknown-eth0"'.IPAddress)
  sleep 30
  nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$SERVER_IP --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_common prepare
  nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$SERVER_IP --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_read_write run > mysql-wo-b4ns-direct.log

  nerdctl rm -f mysql-server
  nerdctl rm -f mysql-client
)

echo "===== Benchmark: mysql client(w/o bypass4netns) server(w/o bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f mysql-server
  nerdctl rm -f mysql-client
  set -ex

  nerdctl run -d -p 13306:3306 --name mysql-server -e MYSQL_ROOT_PASSWORD=pass -e MYSQL_DATABASE=bench $MYSQL_IMAGE
  nerdctl run -d --name mysql-client $BENCH_IMAGE sleep infinity
  sleep 30
  nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$HOST_IP --mysql-port=13306 --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_common prepare
  nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$HOST_IP --mysql-port=13306 --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_read_write run > mysql-wo-b4ns-host.log

  nerdctl rm -f mysql-server
  nerdctl rm -f mysql-client
)

echo "===== Benchmark: mysql client(w/ bypass4netns) server(w/ bypass4netns) via host ====="
(
  set +e
  systemctl --user stop run-bypass4netnsd
  nerdctl rm -f mysql-server
  nerdctl rm -f mysql-client
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit run-bypass4netnsd bypass4netnsd

  nerdctl run --label nerdctl/bypass4netns=true -d -p 13306:3306 --name mysql-server -e MYSQL_ROOT_PASSWORD=pass -e MYSQL_DATABASE=bench $MYSQL_IMAGE
  nerdctl run --label nerdctl/bypass4netns=true -d --name mysql-client $BENCH_IMAGE sleep infinity
  sleep 30
  nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$HOST_IP --mysql-port=13306 --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_common prepare
  nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$HOST_IP --mysql-port=13306 --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_read_write run > mysql-w-b4ns.log

  nerdctl rm -f mysql-server
  nerdctl rm -f mysql-client
  systemctl --user stop run-bypass4netnsd
)

echo "===== Benchmark: mysql client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  set +e
  systemctl --user stop run-bypass4netnsd
  nerdctl rm -f mysql-server
  nerdctl rm -f mysql-client
  systemctl --user stop etcd.service
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$HOST_IP:2379 --advertise-client-urls http://$HOST_IP:2379
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$HOST_IP:2379 --multinode-host-address=$HOST_IP

  nerdctl run --label nerdctl/bypass4netns=true -d -p 13306:3306 --name mysql-server -e MYSQL_ROOT_PASSWORD=pass -e MYSQL_DATABASE=bench $MYSQL_IMAGE
  nerdctl run --label nerdctl/bypass4netns=true -d --name mysql-client $BENCH_IMAGE sleep infinity
  SERVER_IP=$(nerdctl inspect mysql-server | jq -r .[0].NetworkSettings.Networks.'"unknown-eth0"'.IPAddress)
  sleep 30
  nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$SERVER_IP --mysql-port=3306 --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_common prepare
  nerdctl exec mysql-client sysbench --threads=4 --time=60 --mysql-host=$SERVER_IP --mysql-port=3306 --mysql-db=bench --mysql-user=root --mysql-password=pass --db-driver=mysql oltp_read_write run

  nerdctl rm -f mysql-server
  nerdctl rm -f mysql-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
)
