package main

import (
	"regexp"
	"testing"
)

func TestMain_RegexpFloat(t *testing.T) {
	ifconfigOutput := `br0       Link encap:Ethernet  HWaddr 78:28:CA:0F:8B:0A
          inet addr:192.168.78.35  Bcast:192.168.78.255  Mask:255.255.255.0
          inet6 addr: fe80::7a28:caff:fe0f:8b0a/64 Scope:Link
          inet6 addr: 2001:5a8:4298:d800:7a28:caff:fe0f:8b0a/64 Scope:Global
          UP BROADCAST RUNNING MULTICAST  MTU:1500  Metric:1
          RX packets:591245 errors:0 dropped:9258 overruns:0 frame:0
          TX packets:30434 errors:0 dropped:0 overruns:0 carrier:0
          collisions:0 txqueuelen:0
          RX bytes:555177988 (529.4 MiB)  TX bytes:19272833 (18.3 MiB)`

	tests := []struct {
		name     string
		regex    *regexp.Regexp
		expected float64
	}{
		{"RX bytes", rxBytesRe, 555177988},
		{"RX packets", rxPacketsRe, 591245},
		{"RX errors", rxPacketErrorsRe, 0},
		{"RX dropped", rxPacketDropsRe, 9258},
		{"RX overruns", rxPacketOverrunsRe, 0},
		{"RX frame", rxPacketFramesRe, 0},
		{"TX bytes", txBytesRe, 19272833},
		{"TX packets", txPacketsRe, 30434},
		{"TX errors", txPacketErrorsRe, 0},
		{"TX dropped", txPacketDropsRe, 0},
		{"TX overruns", txPacketOverrunsRe, 0},
		{"TX carrier", txPacketCarriersRe, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := regexpFloat(tt.regex, ifconfigOutput)
			if result != tt.expected {
				t.Errorf("regexpFloat() = %v, want %v", result, tt.expected)
			}
		})
	}
}
