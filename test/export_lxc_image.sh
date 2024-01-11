#!/bin/bash

set -eux -o pipefail

IMAGE_NAME=$1

sudo lxc snapshot $IMAGE_NAME snp0
sudo lxc publish $IMAGE_NAME/snp0 --alias $IMAGE_NAME-export --compression zstd
sudo lxc image export $IMAGE_NAME-export /tmp/$IMAGE_NAME-image
