#!/bin/bash
set -eu -o pipefail

cd $(dirname $0)

IMAGE_NAME="block"
COUNT="10"

source ~/.profile
. ../param.bash

./gen_blocks.sh

# sometimes fail to pull images
# this is workaround
# https://github.com/containerd/nerdctl/issues/622
systemctl --user restart containerd
sleep 1
systemctl --user restart buildkit
sleep 3
systemctl --user status --no-pager containerd
systemctl --user status --no-pager buildkit

sudo nerdctl build -f ./Dockerfile -t $IMAGE_NAME .
nerdctl build -f ./Dockerfile -t $IMAGE_NAME .

BLOCK_SIZES=('1k' '32k' '128k' '512k' '1m' '32m' '128m' '512m' '1g')

echo "===== Benchmark: block rooful via NetNS ====="
(
  set +e
  sudo nerdctl rm -f block-server
  sudo nerdctl rm -f block-client
  set -ex

  sudo nerdctl run -d --name block-server -v $(pwd):/var/www/html:ro $IMAGE_NAME nginx -g "daemon off;"
  sudo nerdctl run -d --name block-client $IMAGE_NAME sleep infinity
  SERVER_IP=$(sudo nerdctl exec block-server hostname -i)
  LOG_NAME="block-rootful-direct.log"
  rm -f $LOG_NAME
  for BLOCK_SIZE in ${BLOCK_SIZES[@]}
  do
    sudo nerdctl exec block-client /bench -count $COUNT -thread-num 1 -url http://$SERVER_IP/blk-$BLOCK_SIZE >> $LOG_NAME
  done

  sudo nerdctl rm -f block-server
  sudo nerdctl rm -f block-client
)

echo "===== Benchmark: block rootful via host ====="
(
  set +e
  sudo nerdctl rm -f block-server
  sudo nerdctl rm -f block-client
  set -ex

  sudo nerdctl run -d --name block-server -p 8080:80 -v $(pwd):/var/www/html:ro $IMAGE_NAME nginx -g "daemon off;"
  sudo nerdctl run -d --name block-client $IMAGE_NAME sleep infinity
  LOG_NAME="block-rootful-host.log"
  rm -f $LOG_NAME
  for BLOCK_SIZE in ${BLOCK_SIZES[@]}
  do
    sudo nerdctl exec block-client /bench -count $COUNT -thread-num 1 -url http://$HOST_IP:8080/blk-$BLOCK_SIZE >> $LOG_NAME
  done

  sudo nerdctl rm -f block-server
  sudo nerdctl rm -f block-client
)

echo "===== Benchmark: block client(w/o bypass4netns) server(w/o bypass4netns) via intermediate NetNS ====="
(
  set +e
  nerdctl rm -f block-server
  nerdctl rm -f block-client
  set -ex

  nerdctl run -d --name block-server -v $(pwd):/var/www/html:ro $IMAGE_NAME nginx -g "daemon off;"
  nerdctl run -d --name block-client $IMAGE_NAME sleep infinity
  SERVER_IP=$(nerdctl exec block-server hostname -i)
  LOG_NAME="block-wo-b4ns-direct.log"
  rm -f $LOG_NAME
  for BLOCK_SIZE in ${BLOCK_SIZES[@]}
  do
    nerdctl exec block-client /bench -count $COUNT -thread-num 1 -url http://$SERVER_IP/blk-$BLOCK_SIZE >> $LOG_NAME
  done

  nerdctl rm -f block-server
  nerdctl rm -f block-client
)

echo "===== Benchmark: block client(w/o bypass4netns) server(w/o bypass4netns) via host ====="
(
  set +e
  nerdctl rm -f block-server
  nerdctl rm -f block-client
  set -ex

  nerdctl run -d --name block-server -p 8080:80 -v $(pwd):/var/www/html:ro $IMAGE_NAME nginx -g "daemon off;"
  nerdctl run -d --name block-client $IMAGE_NAME sleep infinity
  LOG_NAME="block-wo-b4ns-host.log"
  rm -f $LOG_NAME
  for BLOCK_SIZE in ${BLOCK_SIZES[@]}
  do
    nerdctl exec block-client /bench -count $COUNT -thread-num 1 -url http://$HOST_IP:8080/blk-$BLOCK_SIZE >> $LOG_NAME
  done

  nerdctl rm -f block-server
  nerdctl rm -f block-client
)

echo "===== Benchmark: block client(w/ bypass4netns) server(w/ bypass4netns) via host ====="
(
  set +e
  systemctl --user stop run-bypass4netnsd
  nerdctl rm -f block-server
  nerdctl rm -f block-client
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit run-bypass4netnsd bypass4netnsd 

  nerdctl run --label nerdctl/bypass4netns=true -d --name block-server -p 8080:80 -v $(pwd):/var/www/html:ro $IMAGE_NAME nginx -g "daemon off;"
  nerdctl run --label nerdctl/bypass4netns=true -d --name block-client $IMAGE_NAME sleep infinity
  LOG_NAME="block-w-b4ns.log"
  rm -f $LOG_NAME
  for BLOCK_SIZE in ${BLOCK_SIZES[@]}
  do
    nerdctl exec block-client /bench -count $COUNT -thread-num 1 -url http://$HOST_IP:8080/blk-$BLOCK_SIZE >> $LOG_NAME
  done

  nerdctl rm -f block-server
  nerdctl rm -f block-client
  systemctl --user stop run-bypass4netnsd
)

echo "===== Benchmark: block client(w/ bypass4netns) server(w/ bypass4netns) with multinode ====="
(
  set +e
  nerdctl rm -f block-server
  nerdctl rm -f block-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
  set -ex

  systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://$HOST_IP:2379 --advertise-client-urls http://$HOST_IP:2379
  systemd-run --user --unit run-bypass4netnsd bypass4netnsd --multinode=true --multinode-etcd-address=http://$HOST_IP:2379 --multinode-host-address=$HOST_IP

  nerdctl run --label nerdctl/bypass4netns=true -d --name block-server -p 8080:80 -v $(pwd):/var/www/html:ro $IMAGE_NAME nginx -g "daemon off;"
  nerdctl run --label nerdctl/bypass4netns=true -d --name block-client $IMAGE_NAME sleep infinity
  SERVER_IP=$(nerdctl exec block-server hostname -i)
  for BLOCK_SIZE in ${BLOCK_SIZES[@]}
  do
    nerdctl exec block-client /bench -count $COUNT -thread-num 1 -url http://$SERVER_IP/blk-$BLOCK_SIZE
  done

  nerdctl rm -f block-server
  nerdctl rm -f block-client
  systemctl --user stop run-bypass4netnsd
  systemctl --user stop etcd.service
  systemctl --user reset-failed
)

