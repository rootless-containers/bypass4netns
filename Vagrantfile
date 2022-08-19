# -*- mode: ruby -*-
# vi: set ft=ruby :

Vagrant.configure("2") do |config|
  config.vm.box = "ubuntu/jammy64"
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

    NERDCTL_VERSION="0.22.2"
    ALPINE_IMAGE="public.ecr.aws/docker/library/alpine:3.16"
    echo "===== Prepare ====="
    (
     set -x
     sudo apt-get update
     sudo DEBIAN_FRONTEND=noninteractive apt-get install -q -y build-essential curl dbus-user-session iperf3 libseccomp-dev uidmap golang python3
     systemctl --user start dbus

     curl -fsSL https://github.com/containerd/nerdctl/releases/download/v${NERDCTL_VERSION}/nerdctl-full-${NERDCTL_VERSION}-linux-amd64.tar.gz | sudo tar Cxzv /usr/local
     containerd-rootless-setuptool.sh install
     containerd-rootless-setuptool.sh install-buildkit
     nerdctl info
     nerdctl pull --quiet "${ALPINE_IMAGE}"

     cd /vagrant
     make
     sudo rm -f /usr/local/bin/bypass4netns*
     sudo make install

     hostname -I | awk '{print $1}' | tee /tmp/host_ip
     /vagrant/test/seccomp.json.sh | tee /tmp/seccomp.json

     systemd-run --user --unit run-iperf3 iperf3 -s
    )

    echo "===== `--ignore` option test ====="
    (
     set -x
     systemd-run --user --unit run-bypass4netns bypass4netns --ignore "127.0.0.0/8,10.0.0.0/8,192.168.6.0/24" --debug
     nerdctl run --security-opt seccomp=/tmp/seccomp.json -d --name test "${ALPINE_IMAGE}" sleep infinity
     nerdctl exec test apk add --no-cache iperf3
     nerdctl exec test iperf3 -c $(cat /tmp/host_ip)
     # TODO: this check is dirty. we want better method to check the connect(2) is ignored.
     journalctl --user -u run-bypass4netns.service | grep "is ignored, skipping."
     nerdctl rm -f test
     systemctl --user stop run-bypass4netns.service

    )

    echo "===== connect(2),sendto(2) test ====="
    (
     systemd-run --user --unit run-bypass4netns bypass4netns --ignore "127.0.0.0/8,10.0.0.0/8" -p 8080:5201
     set -x
     cd /vagrant/test
     /bin/bash test.sh /tmp/seccomp.json $(cat /tmp/host_ip)
     systemctl --user stop run-bypass4netns.service
    )

    echo "===== Test bypass4netnsd ====="
    (
     set -x
     /vagrant/test/test_b4nnd.sh
    )

    echo "===== Benchmark: netns -> host With bypass4netns ====="
    (
     set -x

     # start bypass4netnsd for nerdctl integration
     systemd-run --user --unit run-bypass4netnsd bypass4netnsd
     sleep 1
     nerdctl run --label nerdctl/bypass4netns=true -d --name test "${ALPINE_IMAGE}" sleep infinity
     nerdctl exec test apk add --no-cache iperf3
     nerdctl exec test iperf3 -c "$(cat /tmp/host_ip)"
     nerdctl rm -f test
    )

    echo "===== Benchmark: netns -> host Without bypass4netns (for comparison) ====="
    (
     set -x
     nerdctl run -d --name test "${ALPINE_IMAGE}" sleep infinity
     nerdctl exec test apk add --no-cache iperf3
     nerdctl exec test iperf3 -c "$(cat /tmp/host_ip)"
     nerdctl rm -f test
    )

    echo "===== Benchmark: host -> netns With bypass4netns ====="
    (
     set -x
     nerdctl run --label nerdctl/bypass4netns=true -d --name test -p 8080:5201 "${ALPINE_IMAGE}" sleep infinity
     nerdctl exec test apk add --no-cache iperf3
     systemd-run --user --unit run-iperf3-netns nerdctl exec test iperf3 -s -4
     sleep 1 # waiting `iperf3 -s -4` becomes ready
     iperf3 -c "$(cat /tmp/host_ip)" -p 8080
     nerdctl rm -f test
    )

    echo "===== Benchmark: host -> netns Without bypass4netns (for comparison) ====="
    (
     set -x
     nerdctl run -d --name test -p 8080:5201 "${ALPINE_IMAGE}" sleep infinity
     nerdctl exec test apk add --no-cache iperf3
     systemd-run --user --unit run-iperf3-netns2 nerdctl exec test iperf3 -s -4
     sleep 1
     iperf3 -c "$(cat /tmp/host_ip)" -p 8080
     nerdctl rm -f test
    )

  SHELL
end
