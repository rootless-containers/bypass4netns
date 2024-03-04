FROM golang:1.22.0 as bench-builder

RUN mkdir /etcd
RUN curl -fsSL https://github.com/etcd-io/etcd/archive/refs/tags/v3.5.10.tar.gz | tar -xz --no-same-owner --no-same-permissions --strip-components 1 -C /etcd
RUN ls -l /etcd

# static link
RUN cd /etcd/tools/benchmark && CGO_ENABLED=0 go build -o /bench main.go

FROM ubuntu:22.04

COPY --from=bench-builder /bench /bench

CMD ["/bin/bash", "-c", "sleep infinity"]
