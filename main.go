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
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	fs := flag.NewFlagSet("sonos_exporter", flag.ExitOnError)
	flagAddress := fs.String("address", "localhost:1915", "Listen address")
	flagTargets := fs.String("targets", "", "Sonos target addresses (host:port, comma separated)")

	fs.Parse(os.Args[1:])

	var targets []string
	if *flagTargets != "" {
		for _, t := range strings.Split(*flagTargets, ",") {
			targets = append(targets, "http://"+t+"/xml/device_description.xml")
		}
	}

	c := newCollector(targets)
	prometheus.MustRegister(c.collectionErrors)
	prometheus.MustRegister(c.collectionDuration)
	prometheus.MustRegister(c)

	log.Printf("Sonos exporter listening on %s", *flagAddress)
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(*flagAddress, nil))
}

type collector struct {
	targets []string

	speakerInfo *prometheus.Desc

	rxBytesTotal *prometheus.Desc
	txBytesTotal *prometheus.Desc

	rxPacketsTotal        *prometheus.Desc
	rxPacketErrorsTotal   *prometheus.Desc
	rxPacketDropsTotal    *prometheus.Desc
	rxPacketOverrunsTotal *prometheus.Desc
	rxPacketFramesTotal   *prometheus.Desc
	txPacketsTotal        *prometheus.Desc
	txPacketErrorsTotal   *prometheus.Desc
	txPacketDropsTotal    *prometheus.Desc
	txPacketOverrunsTotal *prometheus.Desc
	txPacketCarriersTotal *prometheus.Desc

	collectionDuration prometheus.Histogram
	collectionErrors   prometheus.Counter
}

func newCollector(targets []string) collector {
	return collector{
		// url:port targets to scrape. If present, disables SSDP search.
		targets: targets,

		speakerInfo: prometheus.NewDesc(
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
		),

		rxBytesTotal: prometheus.NewDesc(
			"sonos_rx_bytes_total", "Received bytes",
			[]string{"player", "device", "serial_num"},
			nil,
		),
		txBytesTotal: prometheus.NewDesc(
			"sonos_tx_bytes_total", "Transmitted bytes",
			[]string{"player", "device", "serial_num"},
			nil,
		),

		rxPacketsTotal: prometheus.NewDesc(
			"sonos_rx_packets_total", "Received packets",
			[]string{"player", "device", "serial_num"},
			nil,
		),
		rxPacketErrorsTotal: prometheus.NewDesc(
			"sonos_rx_packet_errors_total", "Received packet errors",
			[]string{"player", "device", "serial_num"},
			nil,
		),
		rxPacketDropsTotal: prometheus.NewDesc(
			"sonos_rx_packet_drops_total", "Received packet drops",
			[]string{"player", "device", "serial_num"},
			nil,
		),
		rxPacketOverrunsTotal: prometheus.NewDesc(
			"sonos_rx_packet_overruns_total", "Received packet overruns",
			[]string{"player", "device", "serial_num"},
			nil,
		),
		rxPacketFramesTotal: prometheus.NewDesc(
			"sonos_rx_packet_frames_total", "Received packet frame errors",
			[]string{"player", "device", "serial_num"},
			nil,
		),

		txPacketsTotal: prometheus.NewDesc(
			"sonos_tx_packets_total", "Transmitted packets",
			[]string{"player", "device", "serial_num"},
			nil,
		),
		txPacketErrorsTotal: prometheus.NewDesc(
			"sonos_tx_packet_errors_total", "Transmitted packet errors",
			[]string{"player", "device", "serial_num"},
			nil,
		),
		txPacketDropsTotal: prometheus.NewDesc(
			"sonos_tx_packet_drops_total", "Transmitted packet drops",
			[]string{"player", "device", "serial_num"},
			nil,
		),
		txPacketOverrunsTotal: prometheus.NewDesc(
			"sonos_tx_packet_overruns_total", "Transmitted packet overruns",
			[]string{"player", "device", "serial_num"},
			nil,
		),
		txPacketCarriersTotal: prometheus.NewDesc(
			"sonos_tx_packet_carriers_total", "Transmitted packet carrier errors",
			[]string{"player", "device", "serial_num"},
			nil,
		),

		collectionDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "sonos_collection_duration_seconds",
			Help: "Time spent collecting from all devices",
		}),
		collectionErrors: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "sonos_collection_errors_total",
				Help: "Errors observed when collecting devices",
			},
		),
	}
}

// Describe implements Prometheus.Collector.
func (c collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.speakerInfo
	ch <- c.rxBytesTotal
	ch <- c.txBytesTotal
	ch <- c.rxPacketsTotal
	ch <- c.rxPacketErrorsTotal
	ch <- c.rxPacketDropsTotal
	ch <- c.rxPacketOverrunsTotal
	ch <- c.rxPacketFramesTotal
	ch <- c.txPacketsTotal
	ch <- c.txPacketErrorsTotal
	ch <- c.txPacketDropsTotal
	ch <- c.txPacketOverrunsTotal
	ch <- c.txPacketCarriersTotal
}

// Collect implements Prometheus.Collector.
func (c collector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()

	targets := c.targets
	if len(targets) == 0 {
		found, err := Search("urn:schemas-upnp-org:device:ZonePlayer:1")
		if err != nil {
			log.Printf("Search: %s", err)
			c.collectionErrors.Inc()
			return
		}
		targets = append(targets, found...)
	}

	var wg sync.WaitGroup
	wg.Add(len(targets))

	for _, target := range targets {
		go func(target string) {
			c.collect(ch, target)
			wg.Done()
		}(target)
	}

	wg.Wait()

	c.collectionDuration.Observe(time.Since(start).Seconds())
}

// Search performs an SDDP query via multicast.
func Search(query string) ([]string, error) {
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

	var locs []string
	for _, device := range devices {
		locs = append(locs, device.Get("Location"))
	}

	return locs, nil
}

func (c collector) collect(ch chan<- prometheus.Metric, loc string) {
	base, err := url.Parse(loc)
	if err != nil {
		log.Printf("Parse %s: %s", loc, err)
		c.collectionErrors.Inc()
		return
	}

	d, err := fetchDevice(base)
	if err != nil {
		log.Printf("Get info %s: %s", loc, err)
		c.collectionErrors.Inc()
		return
	}

	ch <- prometheus.MustNewConstMetric(
		c.speakerInfo,
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
		c.collectionErrors.Inc()
		return
	}

	for device, stats := range ifaces {
		ch <- prometheus.MustNewConstMetric(
			c.rxBytesTotal,
			prometheus.CounterValue,
			stats.rxBytes,
			d.RoomName,
			device,
			d.SerialNum,
		)

		ch <- prometheus.MustNewConstMetric(
			c.rxPacketsTotal,
			prometheus.CounterValue,
			stats.rxPackets,
			d.RoomName,
			device,
			d.SerialNum,
		)

		ch <- prometheus.MustNewConstMetric(
			c.rxPacketErrorsTotal,
			prometheus.CounterValue,
			stats.rxPacketErrors,
			d.RoomName,
			device,
			d.SerialNum,
		)

		ch <- prometheus.MustNewConstMetric(
			c.rxPacketDropsTotal,
			prometheus.CounterValue,
			stats.rxPacketDrops,
			d.RoomName,
			device,
			d.SerialNum,
		)

		ch <- prometheus.MustNewConstMetric(
			c.rxPacketOverrunsTotal,
			prometheus.CounterValue,
			stats.rxPacketOverruns,
			d.RoomName,
			device,
			d.SerialNum,
		)

		ch <- prometheus.MustNewConstMetric(
			c.rxPacketFramesTotal,
			prometheus.CounterValue,
			stats.rxPacketFrames,
			d.RoomName,
			device,
			d.SerialNum,
		)

		ch <- prometheus.MustNewConstMetric(
			c.txBytesTotal,
			prometheus.CounterValue,
			stats.txBytes,
			d.RoomName,
			device,
			d.SerialNum,
		)

		ch <- prometheus.MustNewConstMetric(
			c.txPacketsTotal,
			prometheus.CounterValue,
			stats.txPackets,
			d.RoomName,
			device,
			d.SerialNum,
		)

		ch <- prometheus.MustNewConstMetric(
			c.txPacketErrorsTotal,
			prometheus.CounterValue,
			stats.txPacketErrors,
			d.RoomName,
			device,
			d.SerialNum,
		)

		ch <- prometheus.MustNewConstMetric(
			c.txPacketDropsTotal,
			prometheus.CounterValue,
			stats.txPacketDrops,
			d.RoomName,
			device,
			d.SerialNum,
		)

		ch <- prometheus.MustNewConstMetric(
			c.txPacketOverrunsTotal,
			prometheus.CounterValue,
			stats.txPacketOverruns,
			d.RoomName,
			device,
			d.SerialNum,
		)

		ch <- prometheus.MustNewConstMetric(
			c.txPacketCarriersTotal,
			prometheus.CounterValue,
			stats.txPacketCarriers,
			d.RoomName,
			device,
			d.SerialNum,
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
	//           RX bytes:263284 (257.1 KiB)  TX bytes:263284 (257.1 KiB)

	ret := make(map[string]stats)

	for _, text := range strings.Split(root.Command, "\n\n") {
		if strings.TrimSpace(text) == "" {
			continue
		}

		ifaceName := ifaceNameRe.FindString(text)
		if ifaceName != "" {
			ret[ifaceName] = stats{
				rxBytes:          regexpFloat(rxBytesRe, text),
				rxPackets:        regexpFloat(rxPacketsRe, text),
				rxPacketErrors:   regexpFloat(rxPacketErrorsRe, text),
				rxPacketDrops:    regexpFloat(rxPacketDropsRe, text),
				rxPacketOverruns: regexpFloat(rxPacketOverrunsRe, text),
				rxPacketFrames:   regexpFloat(rxPacketFramesRe, text),
				txBytes:          regexpFloat(txBytesRe, text),
				txPackets:        regexpFloat(txPacketsRe, text),
				txPacketErrors:   regexpFloat(txPacketErrorsRe, text),
				txPacketDrops:    regexpFloat(txPacketDropsRe, text),
				txPacketOverruns: regexpFloat(txPacketOverrunsRe, text),
				txPacketCarriers: regexpFloat(txPacketCarriersRe, text),
			}
		}
	}

	return ret, err
}

func regexpFloat(re *regexp.Regexp, text string) float64 {
	m := re.FindStringSubmatch(text)
	if len(m) > 1 {
		return atof(m[1])
	}
	return 0
}

func atof(num string) float64 {
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	return v
}

type stats struct {
	rxBytes           float64
	rxPackets         float64
	rxPacketErrors    float64
	rxPacketDrops     float64
	rxPacketOverruns  float64
	rxPacketFrames    float64
	txBytes           float64
	txPackets         float64
	txPacketErrors    float64
	txPacketDrops     float64
	txPacketOverruns  float64
	txPacketCarriers  float64
}

var (
	ifaceNameRe = regexp.MustCompile(`^[^ ]+`)

	rxBytesRe         = regexp.MustCompile(`RX.*bytes:(\d+)`)
	rxPacketsRe       = regexp.MustCompile(`RX.*packets:(\d+)`)
	rxPacketErrorsRe  = regexp.MustCompile(`RX.*errors:(\d+)`)
	rxPacketDropsRe   = regexp.MustCompile(`RX.*dropped:(\d+)`)
	rxPacketOverrunsRe = regexp.MustCompile(`RX.*overruns:(\d+)`)
	rxPacketFramesRe  = regexp.MustCompile(`RX.*frame:(\d+)`)
	txBytesRe         = regexp.MustCompile(`TX.*bytes:(\d+)`)
	txPacketsRe       = regexp.MustCompile(`TX.*packets:(\d+)`)
	txPacketErrorsRe  = regexp.MustCompile(`TX.*errors:(\d+)`)
	txPacketDropsRe   = regexp.MustCompile(`TX.*dropped:(\d+)`)
	txPacketOverrunsRe = regexp.MustCompile(`TX.*overruns:(\d+)`)
	txPacketCarriersRe = regexp.MustCompile(`TX.*carrier:(\d+)`)
)
