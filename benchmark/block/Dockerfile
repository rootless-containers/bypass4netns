FROM golang:1.22.0 as bench-builder

COPY bench.go .
# static link
RUN CGO_ENABLED=0 go build -o /bench bench.go

FROM ubuntu:22.04

RUN apt-get update && apt-get upgrade -y
RUN apt-get install -y wget multitime nginx
COPY --from=bench-builder /bench /bench

CMD ["/bin/bash", "-c", "sleep infinity"]

