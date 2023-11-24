#!/bin/bash

sudo lxc rm -f test
sudo lxc rm -f test2

set -eux -o pipefail

TEST1_VXLAN_MAC="02:42:c0:a8:00:1"
TEST1_VXLAN_ADDR="192.168.2.1"
TEST2_VXLAN_MAC="02:42:c0:a8:00:2"
TEST2_VXLAN_ADDR="192.168.2.2"

cd $(dirname $0)
. ./util.sh

sudo lxc launch -c security.nesting=true images:ubuntu/22.04 test
sudo lxc launch -c security.nesting=true images:ubuntu/22.04 test2

sleep 5

TEST_ADDR=$(NAME="test" exec_lxc hostname -I)
TEST2_ADDR=$(NAME="test2" exec_lxc hostname -I)

NAME="test" exec_lxc sudo apt install -y ethtool
NAME="test" exec_lxc sudo ip link add vxlan0 type vxlan id 100 noproxy nolearning remote $TEST2_ADDR dstport 4789 dev eth0
NAME="test" exec_lxc sudo ethtool -K vxlan0 tx-checksum-ip-generic off
NAME="test" exec_lxc sudo ip a add $TEST1_VXLAN_ADDR/24 dev vxlan0
NAME="test" exec_lxc sudo ip link set vxlan0 up

NAME="test2" exec_lxc sudo apt install -y ethtool
NAME="test2" exec_lxc sudo ip link add vxlan0 type vxlan id 100 noproxy nolearning remote $TEST_ADDR dstport 4789 dev eth0
NAME="test2" exec_lxc sudo ethtool -K vxlan0 tx-checksum-ip-generic off
NAME="test2" exec_lxc sudo ip a add $TEST2_VXLAN_ADDR/24 dev vxlan0
NAME="test2" exec_lxc sudo ip link set vxlan0 up

NAME="test" exec_lxc ping -c 5 $TEST2_VXLAN_ADDR

sudo lxc rm -f test
sudo lxc rm -f test2