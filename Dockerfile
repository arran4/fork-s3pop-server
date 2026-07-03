FROM alpine:3.20.3

RUN apk --no-cache add ca-certificates && \
    addgroup -S s3pop && \
    adduser -S s3pop -G s3pop

USER s3pop
WORKDIR /home/s3pop

COPY s3pop-server /usr/local/bin/s3pop-server

ENTRYPOINT ["/usr/local/bin/s3pop-server"]
