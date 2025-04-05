FROM scratch
ENTRYPOINT ["/sonos_exporter"]
COPY sonos_exporter /
