package main

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"flag"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	flagAddress = flag.String("address", "localhost:1915", "Listen address")

	collectionDuration = prometheus.NewDesc(
		"sonos_collection_duration",
		"Total collection time",
		nil,
		nil,
	)

	collectionErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "sonos_collection_errors_total",
			Help: "Errors observed when collecting devices",
		},
	)

	speakerInfo = prometheus.NewDesc(
		"sonos_speaker", "Sonos speaker info",
		[]string{
			"room_name",
			"display_version",
			"hardware_version",
			"model_name",
			"model_number",
			"serial_num",
			"software_version",
			"udn",
		},
		nil,
	)

	rxBytes = prometheus.NewDesc(
		"sonos_rx_bytes", "Received bytes",
		[]string{"player", "device"},
		nil,
	)

	txBytes = prometheus.NewDesc(
		"sonos_tx_bytes", "Transmitted bytes",
		[]string{"player", "device"},
		nil,
	)

	rxPackets = prometheus.NewDesc(
		"sonos_rx_packets", "Received packets",
		[]string{"player", "device"},
		nil,
	)

	txPackets = prometheus.NewDesc(
		"sonos_tx_packets", "Transmitted packets ",
		[]string{"player", "device"},
		nil,
	)
)

func init() {
	prometheus.MustRegister(collectionErrors)
	prometheus.MustRegister(collector{})
}

func main() {
	flag.Parse()

	log.Printf("Sonos exporter listening on %s", *flagAddress)
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(*flagAddress, nil))
}

type collector struct{}

// Describe implements Prometheus.Collector.
func (c collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- prometheus.NewDesc("dummy", "dummy", nil, nil)
}

// Collect implements Prometheus.Collector.
func (c collector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()

	found, err := Search("urn:schemas-upnp-org:device:ZonePlayer:1")
	if err != nil {
		log.Printf("Search: %s", err)
		collectionErrors.Inc()
		return
	}

	var wg sync.WaitGroup
	wg.Add(len(found))

	for _, dev := range found {
		go func(dev http.Header) {
			collect(ch, dev.Get("Location"))
			wg.Done()
		}(dev)
	}

	wg.Wait()

	ch <- prometheus.MustNewConstMetric(
		collectionDuration,
		prometheus.GaugeValue,
		time.Since(start).Seconds(),
	)

}

// Search performs an SDDP query via multicast.
func Search(query string) ([]http.Header, error) {
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := strings.Join([]string{
		"M-SEARCH * HTTP/1.1",
		"HOST: 239.255.255.250:1900",
		"MAN: \"ssdp:discover\"",
		"ST: " + query,
		"MX: 1",
	}, "\r\n")

	addr, err := net.ResolveUDPAddr("udp", "239.255.255.250:1900")
	if err != nil {
		return nil, err
	}

	_, err = conn.WriteTo([]byte(req), addr)
	if err != nil {
		return nil, err
	}

	conn.SetDeadline(time.Now().Add(2 * time.Second))

	var devices []http.Header
	for {
		buf := make([]byte, 65536)

		n, _, err := conn.ReadFrom(buf)
		if err, ok := err.(net.Error); ok && err.Timeout() {
			break
		} else if err != nil {
			log.Printf("ReadFrom error: %s", err)
			break
		}

		r := bufio.NewReader(bytes.NewReader(buf[:n]))

		resp, err := http.ReadResponse(r, &http.Request{})
		if err != nil {
			log.Printf("ReadResponse error: %s", err)
		}
		resp.Body.Close()

		for _, head := range resp.Header["St"] {
			if head == query {
				devices = append(devices, resp.Header)
				break
			}
		}
	}

	return devices, nil
}

func collect(ch chan<- prometheus.Metric, loc string) {
	base, err := url.Parse(loc)
	if err != nil {
		log.Printf("Parse %s: %s", loc, err)
		collectionErrors.Inc()
		return
	}

	d, err := fetchDevice(base)
	if err != nil {
		log.Printf("Get info %s: %s", loc, err)
		collectionErrors.Inc()
		return
	}

	ch <- prometheus.MustNewConstMetric(
		speakerInfo,
		prometheus.GaugeValue,
		1,
		d.RoomName,
		d.DisplayVersion,
		d.HardwareVersion,
		d.ModelName,
		d.ModelNumber,
		d.SerialNum,
		d.SoftwareVersion,
		d.UDN,
	)

	ifaces, err := fetchIfconfig(base)
	if err != nil {
		log.Printf("Get ifconfig %s: %s", loc, err)
		collectionErrors.Inc()
		return
	}

	for device, stats := range ifaces {
		ch <- prometheus.MustNewConstMetric(
			rxBytes,
			prometheus.GaugeValue,
			float64(stats.rxBytes),
			d.RoomName,
			device,
		)

		ch <- prometheus.MustNewConstMetric(
			rxPackets,
			prometheus.GaugeValue,
			float64(stats.rxPackets),
			d.RoomName,
			device,
		)

		ch <- prometheus.MustNewConstMetric(
			txBytes,
			prometheus.GaugeValue,
			float64(stats.txBytes),
			d.RoomName,
			device,
		)

		ch <- prometheus.MustNewConstMetric(
			txPackets,
			prometheus.GaugeValue,
			float64(stats.txPackets),
			d.RoomName,
			device,
		)
	}
}

func fetchDevice(u *url.URL) (*Device, error) {
	resp, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var root struct {
		Device Device `xml:"device"`
	}
	if err = xml.NewDecoder(resp.Body).Decode(&root); err != nil {
		log.Printf("Decode %s: %s", u.String(), err)
	}

	return &root.Device, err
}

type Device struct {
	DeviceType      string `xml:"deviceType"`
	RoomName        string `xml:"roomName"`
	DisplayVersion  string `xml:"displayVersion"`
	HardwareVersion string `xml:"hardwareVersion"`
	ModelName       string `xml:"modelName"`
	ModelNumber     string `xml:"modelNumber"`
	SerialNum       string `xml:"serialNum"`
	SoftwareVersion string `xml:"softwareVersion"`
	UDN             string `xml:"UDN"`
}

func fetchIfconfig(base *url.URL) (map[string]stats, error) {
	u := *base
	u.Path = "/status/ifconfig"

	resp, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var root struct {
		Command string `xml:"Command"`
	}
	if err = xml.NewDecoder(resp.Body).Decode(&root); err != nil {
		log.Printf("Decode %s: %s", u.String(), err)
	}

	// root.Command is a blank line separated series of network interfaces:
	//
	// lo        Link encap:Local Loopback
	//           inet addr:127.0.0.1  Mask:255.0.0.0
	//           UP LOOPBACK RUNNING  MTU:16436  Metric:1
	//           RX packets:1558 errors:0 dropped:0 overruns:0 frame:0
	//           TX packets:1558 errors:0 dropped:0 overruns:0 carrier:0
	//           collisions:0 txqueuelen:0
	//           RX bytes:263284 (257.1 KiB)  TX bytes:263284 (257.1

	ret := make(map[string]stats)

	for _, text := range strings.Split(root.Command, "\n\n") {
		if strings.TrimSpace(text) == "" {
			continue
		}

		var m []string
		var s stats

		m = rxBytesRe.FindStringSubmatch(text)
		if len(m) > 1 {
			s.rxBytes = atoi(m[1])
		}

		m = rxPacketsRe.FindStringSubmatch(text)
		if len(m) > 1 {
			s.rxPackets = atoi(m[1])
		}

		m = txBytesRe.FindStringSubmatch(text)
		if len(m) > 1 {
			s.txBytes = atoi(m[1])
		}

		m = txPacketsRe.FindStringSubmatch(text)
		if len(m) > 1 {
			s.txPackets = atoi(m[1])
		}

		name := ifaceNameRe.FindString(text)
		if name != "" {
			ret[name] = s
		}
	}

	return ret, err
}

func atoi(num string) int {
	v, err := strconv.Atoi(num)
	if err != nil {
		return 0
	}
	return v
}

type stats struct {
	rxBytes   int
	rxPackets int
	txBytes   int
	txPackets int
}

var (
	ifaceNameRe = regexp.MustCompile(`^[^ ]+`)
	rxBytesRe   = regexp.MustCompile(`RX bytes:(\d+)`)
	rxPacketsRe = regexp.MustCompile(`RX packets:(\d+)`)
	txBytesRe   = regexp.MustCompile(`TX bytes:(\d+)`)
	txPacketsRe = regexp.MustCompile(`TX packets:(\d+)`)
)
