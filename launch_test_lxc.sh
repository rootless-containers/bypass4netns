#!/bin/bash

cd $(dirname $0)

# lxd init --auto --storage-backend=btrfs
sudo lxc launch -c security.nesting=true images:ubuntu/22.04 test
sudo lxc config device add test share disk source=$(pwd) path=/host
sudo lxc exec test -- /bin/bash -c "echo 'ubuntu ALL=NOPASSWD: ALL' | EDITOR='tee -a' visudo"
# let user services running
sudo lxc exec test -- sudo --login --user ubuntu /bin/bash -c "sleep 3 && sudo loginctl enable-linger"
