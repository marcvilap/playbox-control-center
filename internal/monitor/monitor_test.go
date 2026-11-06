package monitor

import (
	"strings"
	"testing"
	"time"

	"playbox/internal/sonda"
)

func nuevoTest(t *testing.T) *Monitor {
	cfg := PorDefecto()
	cfg.Visores = cfg.Visores[:2]
	cfg.Visores[0].IP, cfg.Visores[1].IP = "10.0.0.1", "10.0.0.2"
	m, err := Nuevo(cfg, t.TempDir(), "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	ahora := time.Now()
	for _, v := range cfg.Visores {
		m.equipos = append(m.equipos, &EstadoEquipo{ID: v.ID, Tipo: "visor", IP: v.IP, Estado: SinConf, Desde: ahora})
	}
	m.equipos = append(m.equipos, &EstadoEquipo{ID: "ROUTER", Tipo: "router", IP: "10.0.0.254", Estado: SinConf, Desde: ahora})
	return m
}

var (
	ok    = sonda.Resultado{OK: true, RTT: 5 * time.Millisecond}
	lento = sonda.Resultado{OK: true, RTT: 150 * time.Millisecond}
	falla = sonda.Resultado{Err: "sin respuesta"}
)

func tipos(m *Monitor) string {
	var t []string
	for _, e := range m.caja.Recientes(100) {
		t = append(t, e.Dispositivo+":"+e.Tipo)
	}
	return strings.Join(t, " ")
}

func TestVisorPerdidoConOtrosActivos(t *testing.T) {
	m := nuevoTest(t)
	a, b := m.equipos[0], m.equipos[1]
	m.procesar(a, ok, time.Now())
	m.procesar(b, ok, time.Now())
	for i := 0; i < 3; i++ {
		m.procesar(a, falla, time.Now())
	}
	if a.Estado != Offline {
		t.Fatalf("PICO-01 debería estar OFFLINE, está %s", a.Estado)
	}
	if !strings.Contains(tipos(m), "PICO-01:visor_perdido_en_uso") || m.incHoy != 1 {
		t.Fatalf("faltó el incidente: %s", tipos(m))
	}
	m.procesar(a, ok, time.Now())
	if a.Estado != Online || !strings.Contains(tipos(m), "PICO-01:recuperado") {
		t.Fatalf("no se recuperó: %s", tipos(m))
	}
}

func TestTodosApagadosNoEsIncidente(t *testing.T) {
	m := nuevoTest(t)
	a := m.equipos[0]
	m.procesar(a, ok, time.Now()) // PICO-02 nunca responde: sigue SIN CONFIRMACIÓN
	for i := 0; i < 3; i++ {
		m.procesar(a, falla, time.Now())
	}
	if m.incHoy != 0 || !strings.Contains(tipos(m), "PICO-01:sin_respuesta") {
		t.Fatalf("apagar el único visor activo no debe abrir incidente: %s", tipos(m))
	}
}

func TestDosFallosNoBastan(t *testing.T) {
	m := nuevoTest(t)
	a := m.equipos[0]
	m.procesar(a, ok, time.Now())
	m.procesar(a, falla, time.Now())
	m.procesar(a, falla, time.Now())
	m.procesar(a, ok, time.Now())
	if a.Estado != Online || m.incHoy != 0 {
		t.Fatal("dos fallos sueltos no deben marcar OFFLINE")
	}
}

func TestLatenciaAlta(t *testing.T) {
	m := nuevoTest(t)
	a := m.equipos[0]
	for _, r := range []sonda.Resultado{ok, lento, ok, lento, lento} {
		m.procesar(a, r, time.Now())
	}
	if !a.LatenciaAlta || !strings.Contains(tipos(m), "PICO-01:latencia_alta") {
		t.Fatalf("debería avisar de latencia: %s", tipos(m))
	}
	for i := 0; i < 6; i++ {
		m.procesar(a, ok, time.Now())
	}
	if a.LatenciaAlta {
		t.Fatal("la latencia debería volver a normal")
	}
}

func TestParsearEventosWindows(t *testing.T) {
	xml := `<Eventos><Event xmlns="http://schemas.microsoft.com/win/2004/08/events/event"><System>
<Provider Name="Microsoft-Windows-Kernel-PnP"/><EventID>410</EventID><Level>4</Level>
<TimeCreated SystemTime="2026-11-06T17:00:00.123Z"/><EventRecordID>99</EventRecordID>
<Channel>Microsoft-Windows-Kernel-PnP/Configuration</Channel></System>
<EventData><Data Name="DeviceInstanceId">USB\VID_046D</Data></EventData></Event></Eventos>`
	ev := ParsearEventosWindows(xml)
	if len(ev) != 1 || ev[0].System.EventID != 410 || ev[0].System.Provider.Name != "Microsoft-Windows-Kernel-PnP" ||
		ev[0].Datos[0].Valor != `USB\VID_046D` {
		t.Fatalf("%+v", ev)
	}
}

func TestReencontrarPorMAC(t *testing.T) {
	m := nuevoTest(t)
	a := m.equipos[0]
	a.MAC = "5a:12:9c:aa:01:7f"
	a.Estado, a.Desde = Offline, time.Now().Add(-2*time.Minute)
	var guardado string
	m.OnIPCambiada = func(id, ip string) { guardado = id + "=" + ip }
	perdidas := m.gafasPerdidas(60 * time.Second)
	if len(perdidas) != 1 || perdidas[0] != "PICO-01" {
		t.Fatalf("perdidas: %v", perdidas)
	}
	pend := m.aplicarTabla(map[string]string{"10.0.0.99": "5A:12:9C:AA:01:7F", "10.0.0.5": "aa:aa:aa:aa:aa:aa"}, perdidas)
	if len(pend) != 0 || a.IP != "10.0.0.99" || a.Estado != SinConf || guardado != "PICO-01=10.0.0.99" {
		t.Fatalf("no se reencontró: ip=%s estado=%s guardado=%s pend=%v", a.IP, a.Estado, guardado, pend)
	}
	if !strings.Contains(tipos(m), "PICO-01:ip_cambiada") {
		t.Fatal("falta el evento ip_cambiada")
	}
}
