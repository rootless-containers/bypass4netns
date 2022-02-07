#!/bin/bash

set -x

SECCOMP_CONFIG_PATH=$1
HOST_IP=$2

ALPINE_IMAGE="alpine_test:connect"
nerdctl image build --file Dockerfile_connect -t $ALPINE_IMAGE --no-cache .
nerdctl run --security-opt seccomp=$SECCOMP_CONFIG_PATH -d --name connect_test1 "${ALPINE_IMAGE}" sleep infinity
nerdctl run --security-opt seccomp=$SECCOMP_CONFIG_PATH -d --name connect_test2 "${ALPINE_IMAGE}" sleep infinity
nerdctl exec connect_test1 apk add --no-cache python3
nerdctl exec connect_test2 apk add --no-cache python3

NETNS_IP=`nerdctl exec connect_test2 hostname -i`
echo $NETNS_IP

# test tcp
python3 test_connect.py -s -p 8888  &> /dev/null &
nerdctl exec connect_test2 python3 /tmp/test_connect.py -s -p 8888 &> /dev/null &
nerdctl exec connect_test1 python3 /tmp/test_connect.py -c -p 8888 --host-ip $HOST_IP --netns-ip $NETNS_IP

# test udp
python3 test_connect.py -s -p 8888 -u &> /tmp/test_connect_host &
nerdctl exec connect_test2 python3 /tmp/test_connect.py -s -p 8888 -u &> /tmp/test_connect_test2 &
nerdctl exec connect_test1 python3 /tmp/test_connect.py -c -p 8888 --host-ip $HOST_IP --netns-ip $NETNS_IP -u
sleep 5

nerdctl rm -f connect_test1
nerdctl rm -f connect_test2

# check server is not timedout
cat /tmp/test_connect_host /tmp/test_connect_test2 | grep 'timeout'
if [ $? -eq 0 ]; then
    echo "test connect over udp failed"
    exit 1
fi

rm /tmp/test_connect_host /tmp/test_connect_test2

