#!/bin/bash

set -eu -o pipefail

TEST_USER=ubuntu

if [ "$(whoami)" != "$TEST_USER" ]; then
    su $TEST_USER -c $0
    exit 0
fi

GO_VERSION="1.21.4"
NERDCTL_VERSION="1.7.0"

echo "===== Prepare ====="
(
  set -x
 
  sudo cp -r /host ~/bypass4netns
  sudo chown -R $TEST_USER:$TEST_USER ~/bypass4netns

  sudo apt-get update
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -q -y build-essential curl dbus-user-session iperf3 libseccomp-dev uidmap python3 pkg-config iptables etcd jq tcpdump ethtool python3-pip
  pip3 install matplotlib numpy
  sudo systemctl stop etcd
  sudo systemctl disable etcd

  systemctl --user start dbus

  curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz | sudo tar Cxz /usr/local
  echo "export PATH=$PATH:/usr/local/go/bin" >> ~/.profile
  source ~/.profile

  curl -fsSL https://github.com/containerd/nerdctl/releases/download/v${NERDCTL_VERSION}/nerdctl-full-${NERDCTL_VERSION}-linux-amd64.tar.gz | sudo tar Cxz /usr/local
  containerd-rootless-setuptool.sh install
  containerd-rootless-setuptool.sh install-buildkit

  # build nerdctl with bypass4netns
  curl -fsSL https://github.com/containerd/nerdctl/archive/refs/tags/v${NERDCTL_VERSION}.tar.gz | tar Cxz ~/
  cd ~/nerdctl-${NERDCTL_VERSION}
  echo "replace github.com/rootless-containers/bypass4netns => /home/$TEST_USER/bypass4netns" >> go.mod
  make
  sudo rm -f /usr/local/bin/nerdctl
  sudo cp _output/nerdctl /usr/local/bin/nerdctl
  nerdctl info

  cd ~/bypass4netns
  make
  sudo rm -f /usr/local/bin/bypass4netns*
  sudo make install
)
