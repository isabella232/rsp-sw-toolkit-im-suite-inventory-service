FROM alpine:3.7 as builder

RUN echo http://nl.alpinelinux.org/alpine/v3.7/main > /etc/apk/repositories; \
    echo http://nl.alpinelinux.org/alpine/v3.7/community >> /etc/apk/repositories
    
RUN apk --no-cache add zeromq util-linux bash


FROM busybox:1.30.1

COPY --from=builder /lib/libc.musl-x86_64.so.1 /lib/
COPY --from=builder /lib/ld-musl-x86_64.so.1 /lib/
COPY --from=builder /lib/libcrypto.so.42.0.0 /lib/
COPY --from=builder /lib/libcrypto.so.42 /lib/
COPY --from=builder /usr/lib/libzmq.so.5.1.5 /usr/lib/
COPY --from=builder /usr/lib/libcrypto.so.42 /usr/lib/
COPY --from=builder /usr/lib/libcrypto.so.42.0.0 /usr/lib/

ADD inventory-service /
HEALTHCHECK --interval=5s --timeout=3s CMD ["/inventory-service","-isHealthy"]

ARG GIT_COMMIT=unspecified
LABEL git_commit=$GIT_COMMIT

ENTRYPOINT ["/inventory-service"]