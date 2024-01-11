#!/bin/bash

cd $(dirname $0)

BLOCK_SIZES=(1 32 128 512)

for BLOCK_SIZE in ${BLOCK_SIZES[@]}
do
    dd if=/dev/urandom of=blk-${BLOCK_SIZE}k bs=1024 count=$BLOCK_SIZE
done

for BLOCK_SIZE in ${BLOCK_SIZES[@]}
do
    dd if=/dev/urandom of=blk-${BLOCK_SIZE}m bs=1048576 count=$BLOCK_SIZE
done

dd if=/dev/urandom of=blk-1g bs=1048576 count=1024

