#!/bin/bash

set -eu -o pipefail

function exec_netns() {
    nsenter -t $PID -F -U --preserve-credentials -n -- "$@"
}

function exec_lxc() {
    sudo lxc exec $NAME -- sudo --login --user ubuntu "$@"
}