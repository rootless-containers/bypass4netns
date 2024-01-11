#!/bin/bash

set -eux -o pipefail

PID=$(nerdctl inspect $1 | jq '.[0].State.Pid')
nsenter -t $PID -F -U --preserve-credentials -n
