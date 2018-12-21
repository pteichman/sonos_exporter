sonos_exporter
==============

This one is short and sweet: it exports network stats from each device
in a Sonos network to Prometheus.

It autodetects the Sonos network, requests ifconfig information from
each host, and ships the stats out via the official Prometheus client.

Dependencies are tracked with dep and vendored in this repository.

Running the exporter:

    $ go build .
    $ ./sonos_exporter
    $ curl http://localhost:1915/metrics

You can bind to another address and port with the --address flag.

It exports these stats:

    * sonos_rx_packets
    * sonos_tx_packets
    * sonos_rx_bytes
    * sonos_tx_bytes

They'll be labeled with the Sonos zone name ("player") and network
device ("device").
