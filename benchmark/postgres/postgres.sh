#!/bin/bash

set -eu -o pipefail

POSTGRES_VERSION=16.1
POSTGRES_IMAGE="postgres:$POSTGRES_VERSION"

source ~/.profile
cd $(dirname $0)
. ../../util.sh
. ../param.bash

sudo nerdctl pull --quiet $POSTGRES_IMAGE
nerdctl pull --quiet $POSTGRES_IMAGE

echo "===== Benchmark: postgresql rootful via NetNS ====="
(
  set +e
  sudo nerdctl rm -f psql-server
  sudo nerdctl rm -f psql-client
  set -ex

  sudo nerdctl run -d --name psql-server -e POSTGRES_PASSWORD=pass $POSTGRES_IMAGE
  sudo nerdctl run -d --name psql-client -e PGPASSWORD=pass $POSTGRES_IMAGE sleep infinity
  SERVER_IP=$(sudo nerdctl exec psql-server hostname -i)
  sleep 5
  sudo nerdctl exec psql-client pgbench -h $SERVER_IP -U postgres -s 10 -i postgres
  sudo nerdctl exec psql-client pgbench -h $SERVER_IP -U postgres -s 10 -t 1000 postgres > postgres-rootful-direct.log

  sudo nerdctl rm -f psql-server
  sudo nerdctl rm -f psql-client
)

echo "===== Benchmark: postgresql rootful via host ====="
(
  set +e
  sudo nerdctl rm -f psql-server
  sudo nerdctl rm -f psql-client
  set -ex

  sudo nerdctl run -d -p 15432:5432 --name psql-server -e POSTGRES_PASSWORD=pass $POSTGRES_IMAGE
  sudo nerdctl run -d --name psql-client -e PGPASSWORD=pass $POSTGRES_IMAGE sleep infinity
  sleep 5
  sudo nerdctl exec psql-client pgbench -h $HOST_IP -p 15432 -U postgres -s 10 -i postgres
  sudo nerdctl exec psql-client pgbench -h $HOST_IP -p 15432 -U postgres -s 10 -t 1000 postgres > postgres-rootful-host.log

  sudo nerdctl rm -f psql-server
  sudo nerdctl rm -f psql-client
)

echo "===== Benchmark: postgresql client(w/o bypass4netns) server(w/o bypass4netns) via intermediate NetNS ====="
(
  set +e
  nerdctl rm -f psql-server
  nerdctl rm -f psql-client
  set -ex

  nerdctl run -d --name psql-server -e POSTGRES_PASSWORD=pass $POSTGRES_IMAGE
  nerdctl run -d --name psql-client -e PGPASSWORD=pass $POSTGRES_IMAGE sleep infinity
  SERVER_IP=$(nerdctl exec psql-server hostname -i)
  PID=$(nerdctl inspect psql-client | jq '.[0].State.Pid')
  NAME="psql-client" exec_netns /bin/bash -c "until nc -z $SERVER_IP 5432; do sleep 1; done"
  nerdctl exec psql-client pgbench -h $SERVER_IP -U postgres -s 10 -i postgres
  nerdctl exec psql-client pgbench -h $SERVER_IP -U postgres -s 10 -t 1000 postgres > postgres-wo-b4ns-direct.log

  nerdctl rm -f psql-server
  nerdctl rm -f psql-client
)

echo "===== Benchmark: postgresql client(w/o bypass4netns) server(w/o bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f psql-server
  nerdctl rm -f psql-client
  set -ex

  nerdctl run -d -p 15432:5432 --name psql-server -e POSTGRES_PASSWORD=pass $POSTGRES_IMAGE
  nerdctl run -d --name psql-client -e PGPASSWORD=pass $POSTGRES_IMAGE sleep infinity
  sleep 5
  nerdctl exec psql-client pgbench -h $HOST_IP -p 15432 -U postgres -s 10 -i postgres
  nerdctl exec psql-client pgbench -h $HOST_IP -p 15432 -U postgres -s 10 -t 1000 postgres > postgres-wo-b4ns-host.log

  nerdctl rm -f psql-server
  nerdctl rm -f psql-client
)

echo "===== Benchmark: postgresql client(w/ bypass4netns) server(w/ bypass4netns) via host ====="
(
  set +e
  systemctl --user stop run-bypass4netnsd
  nerdctl rm -f psql-server
  nerdctl rm -f psql-client
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit run-bypass4netnsd bypass4netnsd

  nerdctl run --label nerdctl/bypass4netns=true -d -p 15432:5432 --name psql-server -e POSTGRES_PASSWORD=pass $POSTGRES_IMAGE
  nerdctl run --label nerdctl/bypass4netns=true -d --name psql-client -e PGPASSWORD=pass $POSTGRES_IMAGE sleep infinity
  PID=$(nerdctl inspect psql-client | jq '.[0].State.Pid')
  NAME="psql-client" exec_netns /bin/bash -c "until nc -z $HOST_IP 15432; do sleep 1; done"
  nerdctl exec psql-client pgbench -h $HOST_IP -p 15432 -U postgres -s 10 -i postgres
  nerdctl exec psql-client pgbench -h $HOST_IP -p 15432 -U postgres -s 10 -t 1000 postgres > postgres-w-b4ns.log

  nerdctl rm -f psql-server
  nerdctl rm -f psql-client
  systemctl --user stop run-bypass4netnsd
)

echo "===== Benchmark: postgres client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  set +e
  systemctl --user stop run-bypass4netnsd
  nerdctl rm -f psql-server
  nerdctl rm -f psql-client
  systemctl --user stop etcd.service
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$HOST_IP:2379 --advertise-client-urls http://$HOST_IP:2379
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$HOST_IP:2379 --multinode-host-address=$HOST_IP

  nerdctl run --label nerdctl/bypass4netns=true -d -p 15432:5432 --name psql-server -e POSTGRES_PASSWORD=pass $POSTGRES_IMAGE
  nerdctl run --label nerdctl/bypass4netns=true -d --name psql-client -e PGPASSWORD=pass $POSTGRES_IMAGE sleep infinity
  SERVER_IP=$(nerdctl exec psql-server hostname -i)
  sleep 5
  nerdctl exec psql-client pgbench -h $SERVER_IP -p 5432 -U postgres -s 10 -i postgres
  nerdctl exec psql-client pgbench -h $SERVER_IP -p 5432 -U postgres -s 10 -t 1000 postgres

  nerdctl rm -f psql-server
  nerdctl rm -f psql-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
)
