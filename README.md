sonos_exporter
==============

This one is short and sweet: it exports network stats from each device
in a Sonos network to Prometheus.

It autodetects the Sonos network (using the Python SoCo library),
requests ifconfig information from each host, and ships the stats
out via the official Prometheus Python client.

Dependencies are in requirements.txt. This wants Python 3, so be sure
to create your virtualenvs with that.

Creating your virtualenv:

    $ virtualenv -p python3 virtualenv
    $ . virtualenv/bin/activate
    $ pip install -r requirements.txt
    
Running the exporter:

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

From a Container
================

It is useful to run the exporter in a container; unfortunately Sonos discovery mechanism requires host network access. For now running from a container requires to run it on a Linux based system. Docker for Mac will not work.

To run via docker:

``` docker run --net=host -it maxandersen/sonos_exporter --address=0.0.0.0:1915```

The important part is  `--net=host` to make Docker use the host network; and then `--address=0.0.0.0:1915` to tell it to bind to all interfaces. 

You can of course also use your specific host ip, i.e. `192.168.1.10`

There is also a docker compose file that does the same as above. To use it run the following:

```docker-compose up -d```




