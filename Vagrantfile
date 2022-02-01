# -*- mode: ruby -*-
# vi: set ft=ruby :

Vagrant.configure("2") do |config|
  config.vm.box = "ubuntu/impish64"
  memory = 4096
  cpus = 2
  config.vm.provider :virtualbox do |v|
    v.memory = memory
    v.cpus = cpus
    # Avoid 10.0.0.0/8 and 172.0.0.0/8: https://github.com/rootless-containers/bypass4netns/pull/5#issuecomment-1026602768
    v.customize ["modifyvm", :id, "--natnet1", "192.168.6.0/24"]
  end
  config.vm.provider :libvirt do |v|
    v.memory = memory
    v.cpus = cpus
  end
  config.vm.provision "shell", privileged: false, inline: <<~SHELL
    #!/bin/bash
    set -eu -o pipefail

    NERDCTL_VERSION="0.16.1"
    ALPINE_IMAGE="public.ecr.aws/docker/library/alpine:3.15"
    echo "===== Prepare ====="
    (
     set -x
     sudo apt-get update
     sudo DEBIAN_FRONTEND=noninteractive apt-get install -q -y autoconf automake build-essential curl dbus-user-session iperf3 libglib2.0-dev libseccomp-dev uidmap
     systemctl --user start dbus

     cd /vagrant
     autoreconf -fis
     ./configure -q
     make
     sudo make install

     curl -fsSL https://github.com/containerd/nerdctl/releases/download/v${NERDCTL_VERSION}/nerdctl-full-${NERDCTL_VERSION}-linux-amd64.tar.gz | sudo tar Cxzv /usr/local
     containerd-rootless-setuptool.sh install
     nerdctl info
     nerdctl pull --quiet "${ALPINE_IMAGE}"

     hostname -I | awk '{print $1}' | tee /tmp/host_ip
     /vagrant/test/seccomp.json.sh | tee /tmp/seccomp.json

     systemd-run --user --unit run-iperf3 iperf3 -s
     systemd-run --user --unit run-bypass4netns bypass4netns
    )

    echo "===== Benchmark: With bypass4netns ====="
    (
     set -x
     nerdctl run --security-opt seccomp=/tmp/seccomp.json -d --name test "${ALPINE_IMAGE}" sleep infinity
     nerdctl exec test apk add --no-cache iperf3
     nerdctl exec test iperf3 -c "$(cat /tmp/host_ip)"
     nerdctl rm -f test
    )

    echo "===== Benchmark: Without bypass4netns (for comparison) ====="
    (
     set -x
     nerdctl run -d --name test "${ALPINE_IMAGE}" sleep infinity
     nerdctl exec test apk add --no-cache iperf3
     nerdctl exec test iperf3 -c "$(cat /tmp/host_ip)"
     nerdctl rm -f test
    )
  SHELL
end
