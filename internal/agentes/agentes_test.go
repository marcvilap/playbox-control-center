package agentes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func post(t *testing.T, srv *httptest.Server, ruta, auth string, cuerpo any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(cuerpo)
	req, _ := http.NewRequest("POST", srv.URL+base+ruta, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("X-PB-Agent", auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// Mismo diálogo que el agente 0.39 tenía con la web.
func TestProtocoloAgente(t *testing.T) {
	dir := t.TempDir()
	s, err := Nuevo(dir)
	if err != nil {
		t.Fatal(err)
	}
	var eventos []string
	s.OnEvento = func(g, tipo, _, _ string, _ map[string]any) { eventos = append(eventos, tipo) }
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	if c, _ := post(t, ts, "enroll", "", map[string]any{"key": "mala", "uid": "abcdef123"}); c != 403 {
		t.Fatalf("clave mala: %d", c)
	}
	c, alta := post(t, ts, "enroll", "", map[string]any{"key": s.ClaveAlta() + "@girona", "uid": "abcdef123",
		"serial": "PA94Y0MGL6120178G", "model": "PICO A9210", "name": "PICO-01"})
	if c != 200 {
		t.Fatalf("alta: %d %v", c, alta)
	}
	auth := strings.TrimSuffix(strings.TrimSuffix(jsonNum(alta["id"]), ".0"), "") + "." + alta["token"].(string)

	if c, _ := post(t, ts, "heartbeat", "1.tokenfalsotokenfalsotoken", map[string]any{}); c != 401 {
		t.Fatalf("token malo: %d", c)
	}
	if _, err := s.Encolar(1, "ping", nil); err != nil {
		t.Fatal(err)
	}
	estado := map[string]any{
		"battery": map[string]any{"level": 77, "charging": false, "temp": 34.5},
		"screen":  map[string]any{"on": true},
		"wifi":    map[string]any{"ssid": "PlayBox", "rssi": -55, "ip": "192.168.1.52"},
		"app":     map[string]any{"pkg": "com.vex.arena", "label": "VEX Arena"},
		"pico":    map[string]any{"ready": true, "ctrlLeft": 4, "ctrlRight": 1, "ctrlConnected": 3},
	}
	c, r := post(t, ts, "heartbeat", auth, map[string]any{"code": 39, "version": "0.39", "state": estado,
		"events": []any{map[string]any{"kind": "screen_on", "at": 1762400000000}}, "results": []any{}})
	if c != 200 {
		t.Fatalf("latido: %d %v", c, r)
	}
	ords := r["commands"].([]any)
	if len(ords) != 1 || ords[0].(map[string]any)["kind"] != "ping" {
		t.Fatalf("órdenes: %v", r["commands"])
	}
	st := r["settings"].(map[string]any)
	if st["safe"] != true || !strings.Contains(st["signal"].(string), "/playbox-senal/") {
		t.Fatalf("ajustes: %v", st)
	}
	g := s.Gafas()[0]
	if *g.Estado.BateriaPct != 77 || *g.Estado.MandoIzq != 4 || *g.Estado.MandoDer != 1 || g.Estado.WifiIP != "192.168.1.52" || g.Nombre != "PICO-01" {
		t.Fatalf("estado: %+v", g.Estado)
	}
	// Resultado de la orden: deja de mandarse.
	id := ords[0].(map[string]any)["id"]
	_, r = post(t, ts, "heartbeat", auth, map[string]any{"code": 39, "state": estado, "results": []any{map[string]any{"id": id, "ok": true, "message": "pong"}}})
	if len(r["commands"].([]any)) != 0 || len(s.Resultados()) != 1 {
		t.Fatalf("la orden sigue: %v", r["commands"])
	}
	quiere := map[string]bool{"agente_alta": true, "agente_screen_on": true, "mando_pilas_bajas": true, "orden_resultado": true}
	for _, e := range eventos {
		delete(quiere, e)
	}
	if len(quiere) > 0 {
		t.Fatalf("faltan eventos %v (hay %v)", quiere, eventos)
	}
	// Otro proceso lee el registro guardado (sin tokens).
	s.Cerrar()
	if gs := GafasGuardadas(dir); len(gs) != 1 || gs[0].TokenHash != "" {
		t.Fatalf("registro: %+v", gs)
	}
	s2, _ := Nuevo(dir)
	if s2.ClaveAlta() != s.ClaveAlta() {
		t.Fatal("la clave de alta cambió")
	}
}

func jsonNum(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestMandoSinDato(t *testing.T) {
	e := Limpiar(map[string]any{"pico": map[string]any{"ctrlLeft": 0, "ctrlRight": 7}})
	if e.MandoIzq != nil || e.MandoDer != nil {
		t.Fatal("0 y fuera de rango deben ser «sin dato»")
	}
}
