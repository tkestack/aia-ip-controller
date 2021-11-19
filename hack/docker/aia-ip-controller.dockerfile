FROM ccr.ccs.tencentyun.com/ploto/alpine:3.10

RUN echo "hosts: files dns" >> /etc/nsswitch.conf

ARG ROOT_PACKAGE
RUN mkdir -p /app/bin/
WORKDIR /app
COPY target/aia-ip-controller /app/bin/

ENTRYPOINT ["/app/bin/aia-ip-controller"]