FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY s3pop-server /usr/local/bin/s3pop-server
ENTRYPOINT ["s3pop-server"]
