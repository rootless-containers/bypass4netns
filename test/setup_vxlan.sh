#!/bin/bash

cd $(dirname $0)
. ../util.sh

set -eux -o pipefail

TARGET_CONTAINER=$1
LOCAL_VXLAN_MAC=$2
LOCAL_VXLAN_ADDR=$3
REMOTE_ADDR=$4
REMOTE_VXLAN_MAC=$5
REMOTE_VXLAN_ADDR=$6

sleep 1
# thanks to https://blog.tiqwab.com/2021/07/11/linux-network-vxlan.html
PID=$(nerdctl inspect $TARGET_CONTAINER | jq '.[0].State.Pid')

PID=$PID exec_netns ip link add br0 type bridge
PID=$PID exec_netns ip a add $LOCAL_VXLAN_ADDR/24 dev br0
PID=$PID exec_netns ip link set dev br0 address $LOCAL_VXLAN_MAC
PID=$PID exec_netns ip link set dev br0 up
PID=$PID exec_netns ip link add vxlan0 type vxlan id 100 noproxy nolearning remote $REMOTE_ADDR dstport 4789 dev eth0
PID=$PID exec_netns ip link set vxlan0 master br0
PID=$PID exec_netns ethtool -K vxlan0 tx-checksum-ip-generic off
PID=$PID exec_netns ip link set dev vxlan0 up
PID=$PID exec_netns ip neigh add $REMOTE_VXLAN_ADDR lladdr $REMOTE_VXLAN_MAC dev br0
PID=$PID exec_netns bridge fdb add $REMOTE_VXLAN_MAC dev vxlan0 self dst $REMOTE_ADDR vni 100 port 4789
