#!/bin/bash

set -e

cd $(dirname $0)

BENCHMARKS=(iperf3 block redis memcached etcd rabbitmq mysql postgres)

for BENCH in ${BENCHMARKS[@]}; do
    pushd $BENCH
    ./${BENCH}.sh
    python3 ${BENCH}_plot.py $BENCH-rootful-direct.log $BENCH-rootful-host.log $BENCH-wo-b4ns-direct.log $BENCH-wo-b4ns-host.log $BENCH-w-b4ns.log ../$BENCH.png
    popd
done