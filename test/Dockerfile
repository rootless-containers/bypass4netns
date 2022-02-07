FROM public.ecr.aws/docker/library/alpine:3.15

RUN apk add python3

ADD ./test_connect.py /tmp/test_connect.py
ADD ./test_sendto.py /tmp/test_sendto.py

CMD ["/bin/sh" "-c" "sleep infinity"]
