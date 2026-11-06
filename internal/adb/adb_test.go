package adb

import "testing"

func TestInterpretar(t *testing.T) {
	var g Gafa
	Interpretar(&g, `3: wlan0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc mq state UP qlen 3000
    link/ether 5A:12:9C:AA:01:7F brd ff:ff:ff:ff:ff:ff
    inet 192.168.1.37/24 brd 192.168.1.255 scope global wlan0`,
		`Wifi is enabled
Wifi is connected to "PlayBox_5G"
WifiInfo: SSID: "PlayBox_5G", BSSID: aa:bb:cc:dd:ee:ff, MAC: 5a:12:9c:aa:01:7f, RSSI: -48`)
	if g.MAC != "5a:12:9c:aa:01:7f" || g.IP != "192.168.1.37" || g.SSID != "PlayBox_5G" {
		t.Fatalf("%+v", g)
	}
}
