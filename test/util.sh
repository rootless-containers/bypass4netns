#!/bin/bash

set -eu -o pipefail

function exec_netns() {
    if [ $EUID -eq 0 ]; then
        nsenter -t $PID -F -n -- "$@"
    else
        nsenter -t $PID -F -U --preserve-credentials -n -- "$@"
    fi
}

function exec_lxc() {
    sudo lxc exec $NAME -- sudo --login --user ubuntu "$@"
}