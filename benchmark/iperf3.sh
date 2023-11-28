#!/bin/bash

set -eu -o pipefail

cd $(dirname $0)

ALPINE_IMAGE="public.ecr.aws/docker/library/alpine:3.16"

source ~/.profile

nerdctl pull --quiet $ALPINE_IMAGE

echo "===== Benchmark: iperf3 client(w/o bypass4netns) server(w/o bypass4netns) via intermediate NetNS ====="
(
  set +e
  nerdctl rm -f iperf3-server
  nerdctl rm -f iperf3-client
  systemctl --user stop iperf3-server
  systemctl --user reset-failed
  set -ex

  nerdctl run -d --name iperf3-server $ALPINE_IMAGE sleep infinity
  nerdctl exec iperf3-server apk add --no-cache iperf3
  nerdctl run -d --name iperf3-client $ALPINE_IMAGE sleep infinity
  nerdctl exec iperf3-client apk add --no-cache iperf3

  systemd-run --user --unit iperf3-server nerdctl exec iperf3-server iperf3 -s

  SERVER_IP=$(nerdctl exec iperf3-server hostname -i)
  sleep 1
  nerdctl exec iperf3-client iperf3 -c $SERVER_IP -i 0 --connect-timeout 1000 -J > iperf3-wo-b4ns-direct.log

  nerdctl rm -f iperf3-server
  nerdctl rm -f iperf3-client
  systemctl --user stop iperf3-server
  systemctl --user reset-failed
)

echo "===== Benchmark: iperf3 client(w/o bypass4netns) server(w/o bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f iperf3-server
  nerdctl rm -f iperf3-client
  systemctl --user stop iperf3-server
  systemctl --user reset-failed
  set -ex

  nerdctl run -d --name iperf3-server -p 5202:5201 $ALPINE_IMAGE sleep infinity
  nerdctl exec iperf3-server apk add --no-cache iperf3
  nerdctl run -d --name iperf3-client $ALPINE_IMAGE sleep infinity
  nerdctl exec iperf3-client apk add --no-cache iperf3

  systemd-run --user --unit iperf3-server nerdctl exec iperf3-server iperf3 -s

  SERVER_IP=$(hostname -I | awk '{print $1}')
  sleep 1
  nerdctl exec iperf3-client iperf3 -c $SERVER_IP -p 5202 -i 0 --connect-timeout 1000 -J > iperf3-wo-b4ns-host.log

  nerdctl rm -f iperf3-server
  nerdctl rm -f iperf3-client
  systemctl --user stop iperf3-server
  systemctl --user reset-failed
)

echo "===== Benchmark: iperf3 client(w/ bypass4netns) server(w/ bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f iperf3-server
  nerdctl rm -f iperf3-client
  systemctl --user stop iperf3-server
  systemctl --user stop run-bypass4netnsd
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit run-bypass4netnsd bypass4netnsd 

  nerdctl run --label nerdctl/bypass4netns=true -d --name iperf3-server -p 5202:5201 $ALPINE_IMAGE sleep infinity
  nerdctl exec iperf3-server apk add --no-cache iperf3
  nerdctl run --label nerdctl/bypass4netns=true -d --name iperf3-client $ALPINE_IMAGE sleep infinity
  nerdctl exec iperf3-client apk add --no-cache iperf3

  systemd-run --user --unit iperf3-server nerdctl exec iperf3-server iperf3 -s

  SERVER_IP=$(hostname -I | awk '{print $1}')
  sleep 1
  nerdctl exec iperf3-client iperf3 -c $SERVER_IP -p 5202 -i 0 --connect-timeout 1000 -J > iperf3-w-b4ns.log

  nerdctl rm -f iperf3-server
  nerdctl rm -f iperf3-client
  systemctl --user stop iperf3-server
  systemctl --user stop run-bypass4netnsd
  systemctl --user reset-failed
)

echo "===== Benchmark: iperf3 client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  set +e
  nerdctl rm -f iperf3-server
  nerdctl rm -f iperf3-client
  systemctl --user stop iperf3-server
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
  set -ex

  HOST_IP=$(hostname -I | awk '{print $1}')
  systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://${HOST_IP}:2379 --advertise-client-urls http://${HOST_IP}:2379
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$HOST_IP:2379 --multinode-host-address=$HOST_IP

  nerdctl run --label nerdctl/bypass4netns=true -d --name iperf3-server -p 5202:5201 $ALPINE_IMAGE sleep infinity
  nerdctl exec iperf3-server apk add --no-cache iperf3
  nerdctl run --label nerdctl/bypass4netns=true -d --name iperf3-client $ALPINE_IMAGE sleep infinity
  nerdctl exec iperf3-client apk add --no-cache iperf3

  systemd-run --user --unit iperf3-server nerdctl exec iperf3-server iperf3 -s

  SERVER_IP=$(nerdctl exec iperf3-server hostname -i)
  sleep 1
  nerdctl exec iperf3-client iperf3 -c $SERVER_IP -i 0 --connect-timeout 1000

  nerdctl rm -f iperf3-server
  nerdctl rm -f iperf3-client
  systemctl --user stop iperf3-server
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
)
