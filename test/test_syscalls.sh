#!/bin/bash

set -eu -o pipefail

SECCOMP_CONFIG_PATH=$1
HOST_IP=$2
TEST_CONTAINER_1=test_1
TEST_CONTAINER_2=test_2

ALPINE_IMAGE="alpine_test:connect"
nerdctl image build --file Dockerfile -t $ALPINE_IMAGE --no-cache .
nerdctl run --security-opt seccomp=$SECCOMP_CONFIG_PATH -d --name $TEST_CONTAINER_1 "${ALPINE_IMAGE}" sleep infinity
nerdctl run --security-opt seccomp=$SECCOMP_CONFIG_PATH -d --name $TEST_CONTAINER_2 "${ALPINE_IMAGE}" sleep infinity

NETNS_IP=`nerdctl exec $TEST_CONTAINER_2 hostname -i`
echo $NETNS_IP

echo "test_connect starting..."
# test_connect tcp
python3 test_connect.py -s -p 8888 --count 2 &> /dev/null &
nerdctl exec $TEST_CONTAINER_2 python3 /tmp/test_connect.py -s -p 8888 --count 2 &> /dev/null &
nerdctl exec $TEST_CONTAINER_1 python3 /tmp/test_connect.py -c -p 8888 --host-ip $HOST_IP --netns-ip $NETNS_IP --count 2

## NOTICE ##
# currently, bypass4netns supports only TCP. Tests for udp connections are disabled.
# test_connect udp
#python3 test_connect.py -s -p 8888 -u --count 2 &> /tmp/test_host &
#nerdctl exec $TEST_CONTAINER_2 python3 /tmp/test_connect.py -s -p 8888 -u --count 2 &> /tmp/test_test2 &
#nerdctl exec $TEST_CONTAINER_1 python3 /tmp/test_connect.py -c -p 8888 --host-ip $HOST_IP --netns-ip $NETNS_IP -u --count 2
#sleep 5

# check server is not timedout
#RESULT=`cat /tmp/test_host /tmp/test_test2`
#if [[ "$RESULT" == *timeout* ]]; then
#    echo "test connect over udp failed"
#    cat /tmp/test_host
#    cat /tmp/test_test2
#    exit 1
#fi
echo "test_connect done."

#echo "test_sendto starting..."
## test_sendto tcp
#python3 test_sendto.py -s -p 8888 --count 2 &> /dev/null &
#nerdctl exec $TEST_CONTAINER_2 python3 /tmp/test_sendto.py -s -p 8888 --count 2 &> /dev/null &
#nerdctl exec $TEST_CONTAINER_1 python3 /tmp/test_sendto.py -c -p 8888 --host-ip $HOST_IP --netns-ip $NETNS_IP --count 2
#
## test_sendto udp
#python3 test_sendto.py -s -p 8888 -u --count 2 &> /tmp/test_host &
#nerdctl exec $TEST_CONTAINER_2 python3 /tmp/test_sendto.py -s -p 8888 -u --count 2 &> /tmp/test_test2 &
#nerdctl exec $TEST_CONTAINER_1 python3 /tmp/test_sendto.py -c -p 8888 --host-ip $HOST_IP --netns-ip $NETNS_IP -u --count 2
#sleep 5
#
## check server is not timedout
#RESULT=`cat /tmp/test_host /tmp/test_test2`
#if [[ "$RESULT" == *timeout* ]]; then
#    echo "test sendto over udp failed"
#    cat /tmp/test_host
#    cat /tmp/test_test2
#    exit 1
#fi
#echo "test_sendto done."
#
#echo "test_sendmsg starting..."
## test_sendmsg tcp
#python3 test_sendmsg.py -s -p 8888 --count 2 &> /dev/null &
#nerdctl exec $TEST_CONTAINER_2 python3 /tmp/test_sendmsg.py -s -p 8888 --count 2 &> /dev/null &
#nerdctl exec $TEST_CONTAINER_1 python3 /tmp/test_sendmsg.py -c -p 8888 --host-ip $HOST_IP --netns-ip $NETNS_IP --count 2
#
## test_sendmsg udp
#python3 test_sendmsg.py -s -p 8888 -u --count 2 &> /tmp/test_host &
#nerdctl exec $TEST_CONTAINER_2 python3 /tmp/test_sendmsg.py -s -p 8888 -u --count 2 &> /tmp/test_test2 &
#nerdctl exec $TEST_CONTAINER_1 python3 /tmp/test_sendmsg.py -c -p 8888 --host-ip $HOST_IP --netns-ip $NETNS_IP -u --count 2
#sleep 5
#
## check server is not timedout
#RESULT=`cat /tmp/test_host /tmp/test_test2`
#if [[ "$RESULT" == *timeout* ]]; then
#    echo "test sendto over udp failed"
#    cat /tmp/test_host
#    cat /tmp/test_test2
#    exit 1
#fi
#echo "test_sendmsg done."

nerdctl rm -f $TEST_CONTAINER_2
nerdctl rm -f $TEST_CONTAINER_1
# rm /tmp/test_host /tmp/test_test2
