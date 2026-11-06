package monitor

import (
	"context"
	"encoding/xml"
	"fmt"
	"time"

	"playbox/internal/ejec"
)

// Registros de Windows que interesan (spec §7): errores del sistema,
// conexión/desconexión de dispositivos y Windows Update.
var registrosWindows = []struct{ canal, filtroNivel string }{
	{"System", "(Level=1 or Level=2 or Level=3) and "},
	{"Microsoft-Windows-Kernel-PnP/Configuration", ""},
	{"Microsoft-Windows-WindowsUpdateClient/Operational", ""},
}

type eventoXML struct {
	System struct {
		Provider struct {
			Name string `xml:"Name,attr"`
		} `xml:"Provider"`
		EventID       int    `xml:"EventID"`
		Level         int    `xml:"Level"`
		EventRecordID int64  `xml:"EventRecordID"`
		Channel       string `xml:"Channel"`
		TimeCreated   struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
	} `xml:"System"`
	Datos []struct {
		Nombre string `xml:"Name,attr"`
		Valor  string `xml:",chardata"`
	} `xml:"EventData>Data"`
}

// ParsearEventosWindows interpreta la salida de wevtutil /f:xml /e:Eventos.
func ParsearEventosWindows(txt string) []eventoXML {
	var raiz struct {
		Eventos []eventoXML `xml:"Event"`
	}
	_ = xml.Unmarshal([]byte(ejec.Stdout(txt)), &raiz)
	return raiz.Eventos
}

func severidadWindows(nivel int) string {
	switch nivel {
	case 1:
		return "critica"
	case 2:
		return "alta"
	case 3:
		return "aviso"
	}
	return "info"
}

func (m *Monitor) vigilarWindows(ctx context.Context) {
	if !ejec.EsWindows() {
		return
	}
	vistos := map[string]time.Time{}
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		for _, r := range registrosWindows {
			q := fmt.Sprintf("*[System[%sTimeCreated[timediff(@SystemTime) <= 75000]]]", r.filtroNivel)
			_, txt := ejec.Run(20*time.Second, "wevtutil", "qe", r.canal, "/q:"+q, "/f:xml", "/e:Eventos", "/c:50", "/rd:true")
			for _, ev := range ParsearEventosWindows(txt) {
				clave := fmt.Sprintf("%s/%d", ev.System.Channel, ev.System.EventRecordID)
				if _, ok := vistos[clave]; ok {
					continue
				}
				vistos[clave] = time.Now()
				ts, _ := time.Parse(time.RFC3339Nano, ev.System.TimeCreated.SystemTime)
				datos := map[string]any{"canal": ev.System.Channel, "id_evento": ev.System.EventID,
					"origen": ev.System.Provider.Name, "registro": ev.System.EventRecordID}
				for _, d := range ev.Datos {
					if d.Nombre != "" && len(d.Valor) < 300 {
						datos[d.Nombre] = d.Valor
					}
				}
				m.caja.Registrar(Evento{TS: ts, Fuente: "windows", Tipo: "evento_windows",
					Severidad: severidadWindows(ev.System.Level),
					Mensaje:   fmt.Sprintf("%s · evento %d", ev.System.Provider.Name, ev.System.EventID),
					Datos:     datos})
			}
		}
		for k, v := range vistos {
			if time.Since(v) > 10*time.Minute {
				delete(vistos, k)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
