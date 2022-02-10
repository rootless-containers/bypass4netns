#!/bin/bash

set -eu -o pipefail

SCRIPT_DIR=$(cd $(dirname $0); pwd)
cd $SCRIPT_DIR

cd ../
systemd-run --user --unit run-b4nnd bypass4netnsd
cd cmd/bypass4netnsd/
go test -count=1 .
systemctl --user stop run-b4nnd.service
sleep 1
