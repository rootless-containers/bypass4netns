#!/bin/bash

set -eux -o pipefail

cd $(dirname $0)

#sudo snap remove --purge lxd && sudo snap install lxd --revision=26093
sudo modprobe vxlan
cat test/lxd.yaml | sudo lxd init --preseed
sudo sysctl -w net.ipv4.ip_forward=1
#https://andreas.scherbaum.la/post/2023-01-18_fix-lxc-network-issues-in-ubuntu-22.04/
sudo iptables -I DOCKER-USER -i lxdbr0 -o eth0 -j ACCEPT
sudo iptables -I DOCKER-USER -o lxdbr0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
sudo iptables -F FORWARD
sudo iptables -P FORWARD ACCEPT
