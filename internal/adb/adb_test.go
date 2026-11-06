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

func TestTelemetria(t *testing.T) {
	tel := ParsearTelemetria(`Current Battery Service state:
  AC powered: false
  USB powered: false
  status: 3
  level: 18
  temperature: 384
@@@
  mWakefulness=Awake
@@@
    mResumedActivity: ActivityRecord{1a2b u0 com.pvr.launcher/.MainActivity t3}`)
	if *tel.BateriaPct != 18 || *tel.TempC != 38.4 || tel.Cargando || tel.Pantalla != "Activa" || tel.App != "com.pvr.launcher" || !tel.EnMenu {
		t.Fatalf("%+v", tel)
	}
	tel = ParsearTelemetria("  status: 2\n  level: 77\n@@@\n mWakefulness=Asleep\n@@@\n ResumedActivity: ActivityRecord{x u0 com.vex.arena/.Game t9}")
	if !tel.Cargando || tel.Pantalla != "Reposo" || tel.App != "com.vex.arena" || tel.EnMenu {
		t.Fatalf("%+v", tel)
	}
}
