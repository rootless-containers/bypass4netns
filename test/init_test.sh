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
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -q -y build-essential curl dbus-user-session iperf3 libseccomp-dev uidmap python3 pkg-config iptables etcd
  sudo systemctl stop etcd
  sudo systemctl disable etcd
  HOST_IP=$(hostname -I | sed 's/ //')
  systemd-run --user --unit etcd.service /usr/bin/etcd --listen-client-urls http://${HOST_IP}:2379 --advertise-client-urls http://${HOST_IP}:2379

  systemctl --user start dbus

  curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz | sudo tar Cxz /usr/local
  echo "export PATH=$PATH:/usr/local/go/bin" >> ~/.profile
  source ~/.profile

  curl -fsSL https://github.com/containerd/nerdctl/releases/download/v${NERDCTL_VERSION}/nerdctl-full-${NERDCTL_VERSION}-linux-amd64.tar.gz | sudo tar Cxz /usr/local
  containerd-rootless-setuptool.sh install
  containerd-rootless-setuptool.sh install-buildkit

  containerd-rootless-setuptool.sh install-fuse-overlayfs 
  cat << EOF >> /home/$TEST_USER/.config/containerd/config.toml
[proxy_plugins]
  [proxy_plugins."fuse-overlayfs"]
    type = "snapshot"
    address = "/run/user/1000/containerd-fuse-overlayfs.sock"
EOF

  systemctl restart --user containerd
  echo 'export CONTAINERD_SNAPSHOTTER="fuse-overlayfs"' >> ~/.profile
  source ~/.profile

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

  hostname -I | awk '{print $1}' | tee /tmp/host_ip
  ~/bypass4netns/test/seccomp.json.sh | tee /tmp/seccomp.json

)
