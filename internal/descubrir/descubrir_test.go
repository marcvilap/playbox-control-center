package descubrir

import "testing"

func TestPingWindowsES(t *testing.T) {
	txt := "Haciendo ping a 192.168.0.1 con 32 bytes de datos:\r\n" +
		"Respuesta desde 192.168.0.1: bytes=32 tiempo<1m TTL=64\r\n" +
		"Respuesta desde 192.168.0.1: bytes=32 tiempo=3ms TTL=64\r\n" +
		"Respuesta desde 192.168.0.1: bytes=32 tiempo=2ms TTL=64\r\n\r\n" +
		"Estad\xa1sticas de ping para 192.168.0.1:\r\n" +
		"    Paquetes: enviados = 3, recibidos = 3, perdidos = 0\r\n    (0% perdidos),\r\n" +
		"Tiempos aproximados de ida y vuelta en milisegundos:\r\n" +
		"    M\xa1nimo = 0ms, M\xa0ximo = 3ms, Media = 1ms\r\n"
	p := ParsearPing("192.168.0.1", 3, txt, true)
	if p.PerdidaPct != "0" || p.MinMs != "0" || p.MaxMs != "3" || p.MediaMs != "1" {
		t.Fatalf("resumen mal: %+v", p)
	}
	// tiempos 1, 3, 2 → (|3-1| + |2-3|) / 2 = 1.5
	if p.JitterMs == nil || *p.JitterMs != 1.5 {
		t.Fatalf("jitter mal: %v", p.JitterMs)
	}
}

func TestPingWindowsEN(t *testing.T) {
	txt := "Reply from 10.0.0.5: bytes=32 time=4ms TTL=64\r\nReply from 10.0.0.5: bytes=32 time=6ms TTL=64\r\n" +
		"    Packets: Sent = 2, Received = 2, Lost = 0 (0% loss),\r\n" +
		"    Minimum = 4ms, Maximum = 6ms, Average = 5ms\r\n"
	p := ParsearPing("10.0.0.5", 2, txt, true)
	if p.PerdidaPct != "0" || p.MediaMs != "5" || p.JitterMs == nil || *p.JitterMs != 2 {
		t.Fatalf("%+v", p)
	}
}

func TestVisor(t *testing.T) {
	d := map[string]string{
		"owners":    "1 owner:\nUser 0: admin=ComponentInfo{io.spawnpoint.agent/io.spawnpoint.agent.AdminReceiver},DeviceOwner",
		"bateria":   "Current Battery Service state:\n  AC powered: false\n  USB powered: true\n  level: 87\n  temperature: 312\n",
		"wifi":      "Wifi is connected to \"PlayBox6G\"\nWifiInfo: SSID: \"PlayBox6G\", BSSID: aa, RSSI: -52, Link speed: 2400Mbps, Frequency: 5975MHz",
		"ip":        "3: wlan0: <UP>\n    inet 192.168.0.101/24 brd 192.168.0.255 scope global wlan0",
		"actividad": "  ResumedActivity: ActivityRecord{abc u0 com.herozone.game/.Main t12}",
		"paquetes":  "package:com.herozone.game versionCode:12\npackage:io.spawnpoint.agent versionCode:3\n",
		"serial":    "PA7X0001\n",
	}
	v := ResumirVisor(d)
	if v.DeviceOwner != "io.spawnpoint.agent" || *v.BateriaPct != 87 || *v.TempBateriaC != 31.2 || !v.Cargando ||
		v.RSSIdBm != "-52" || v.FrecuenciaMHz != "5975" || v.SSID != "PlayBox6G" || v.IP != "192.168.0.101" ||
		v.AppPrimerPlano != "com.herozone.game" || v.AppsTerceros != 2 || v.Serial != "PA7X0001" {
		t.Fatalf("%+v", v)
	}
	if ResumirVisor(map[string]string{"owners": "no owners found"}).DeviceOwner != "no owners found" {
		t.Fatal("owner vacío")
	}
}

func TestComoLista(t *testing.T) {
	if len(ComoLista(`{"a":1}`)) != 1 || len(ComoLista(`[{"a":1},{"a":2}]`)) != 2 || ComoLista("") != nil {
		t.Fatal("ComoLista")
	}
	if got := ComoTextos(`"C:\\ProgramData\\Spawnpoint"`); len(got) != 1 {
		t.Fatal(got)
	}
}
