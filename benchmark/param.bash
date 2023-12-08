HOST_IP_PREFIX="192.168.6."
HOST_IP=$(HOST=$(hostname -I); for i in ${HOST[@]}; do echo $i | grep -q $HOST_IP_PREFIX; if [ $? -eq 0 ]; then echo $i; fi; done)
