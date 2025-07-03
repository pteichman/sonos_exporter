FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY sonos_exporter .
CMD ["./sonos_exporter"]
