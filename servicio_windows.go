//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"playbox/internal/agentes"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"playbox/internal/actualizar"
	"playbox/internal/adb"
	"playbox/internal/monitor"
)

const nombreServicio = "PlayBoxMonitor"

func cmdServicio(args []string) int {
	if len(args) == 0 {
		fmt.Println("Uso: playbox servicio instalar | desinstalar | iniciar | parar | ejecutar")
		return 2
	}
	switch args[0] {
	case "ejecutar":
		if es, _ := svc.IsWindowsService(); es {
			if err := svc.Run(nombreServicio, &servicio{}); err != nil {
				return 1
			}
			return 0
		}
		return cmdVigilar(false)
	case "instalar":
		return instalarServicio()
	case "desinstalar":
		return desinstalarServicio()
	case "iniciar":
		return controlar(true)
	case "parar":
		return controlar(false)
	}
	fmt.Println("Orden desconocida:", args[0])
	return 2
}

func conectar() (*mgr.Mgr, bool) {
	m, err := mgr.Connect()
	if err != nil {
		fmt.Println("Necesito permisos de administrador: abre playbox.exe con clic derecho → «Ejecutar como administrador».")
		return nil, false
	}
	return m, true
}

func instalarServicio() int {
	if _, ok := cargarConfig(); !ok {
		return 1
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Println("Error:", err)
		return 1
	}
	m, ok := conectar()
	if !ok {
		return 1
	}
	defer m.Disconnect()
	if err := agentes.CrearReglaDirecta(); err != nil {
		fmt.Println("Aviso: no se pudo abrir el puerto de los agentes en el firewall:", err)
	}
	cfg := mgr.Config{
		DisplayName: "PlayBox VR — Vigilancia",
		Description: "Vigila visores, router e Internet del centro y guarda la caja negra. Solo observa; no actúa sobre juegos ni visores.",
		StartType:   mgr.StartAutomatic,
	}
	s, err := m.OpenService(nombreServicio)
	if err == nil {
		fmt.Println("El servicio ya existía: lo actualizo y lo reinicio.")
		_, _ = s.Control(svc.Stop)
		esperarEstado(s, svc.Stopped)
		cfg.BinaryPathName = fmt.Sprintf("\"%s\" servicio ejecutar", exe)
		if err := s.UpdateConfig(cfg); err != nil {
			fmt.Println("Error:", err)
			s.Close()
			return 1
		}
	} else {
		s, err = m.CreateService(nombreServicio, exe, cfg, "servicio", "ejecutar")
		if err != nil {
			fmt.Println("No se pudo crear el servicio:", err)
			return 1
		}
	}
	defer s.Close()
	// Arranque diferido (no compite con el arranque de Windows); si falla, sigue automático.
	cfg.DelayedAutoStart = true
	if cfg.BinaryPathName == "" {
		cfg.BinaryPathName = fmt.Sprintf("\"%s\" servicio ejecutar", exe)
	}
	_ = s.UpdateConfig(cfg)
	reinicio := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}
	_ = s.SetRecoveryActions(reinicio, 86400)
	_ = s.SetRecoveryActionsOnNonCrashFailures(true)
	if err := s.Start(); err != nil {
		fmt.Println("Instalado, pero no arrancó:", err)
		return 1
	}
	fmt.Println("Servicio «PlayBox VR — Vigilancia» instalado y en marcha.")
	fmt.Println("Arrancará solo con Windows. Consulta el estado con: playbox estado")
	return 0
}

func desinstalarServicio() int {
	m, ok := conectar()
	if !ok {
		return 1
	}
	defer m.Disconnect()
	s, err := m.OpenService(nombreServicio)
	if err != nil {
		fmt.Println("El servicio no está instalado.")
		return 0
	}
	defer s.Close()
	_, _ = s.Control(svc.Stop)
	esperarEstado(s, svc.Stopped)
	if err := s.Delete(); err != nil {
		fmt.Println("Error:", err)
		return 1
	}
	fmt.Println("Servicio desinstalado. Los datos siguen en", dirDatos())
	return 0
}

func controlar(iniciar bool) int {
	m, ok := conectar()
	if !ok {
		return 1
	}
	defer m.Disconnect()
	s, err := m.OpenService(nombreServicio)
	if err != nil {
		fmt.Println("El servicio no está instalado: usa «playbox servicio instalar».")
		return 1
	}
	defer s.Close()
	if iniciar {
		err = s.Start()
	} else {
		_, err = s.Control(svc.Stop)
	}
	if err != nil {
		fmt.Println("Error:", err)
		return 1
	}
	fmt.Println("Hecho.")
	return 0
}

func esperarEstado(s *mgr.Service, objetivo svc.State) {
	for i := 0; i < 30; i++ {
		if st, err := s.Query(); err != nil || st.State == objetivo {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// servicio implementa svc.Handler.
type servicio struct{}

func (servicio) Execute(_ []string, req <-chan svc.ChangeRequest, cambios chan<- svc.Status) (bool, uint32) {
	cambios <- svc.Status{State: svc.StartPending}
	// Prioridad baja: la vigilancia nunca debe quitar CPU a los juegos.
	_ = windows.SetPriorityClass(windows.CurrentProcess(), windows.BELOW_NORMAL_PRIORITY_CLASS)
	actualizar.Limpiar()
	rutaExe, _ := os.Executable()

	log := registroServicio()
	cfg, _, err := monitor.Cargar(rutaConfig())
	if err != nil {
		log("Configuración no válida: " + err.Error())
		return true, 1
	}
	mon, err := monitor.Nuevo(cfg, dirDatos(), Version, log)
	if err != nil {
		log("No se pudo iniciar: " + err.Error())
		return true, 1
	}
	mon.OnIPCambiada = guardarIPCambiada
	mon.RutaADB = adb.Localizar(dirExe())
	mon.PuertoAgentes = agentes.Puerto
	ctx, cancelar := context.WithCancel(context.Background())
	fin := make(chan error, 1)
	go func() { fin <- mon.Ejecutar(ctx) }()
	go actualizarEnReposo(ctx, mon, rutaExe, log)

	cambios <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				cambios <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				cambios <- svc.Status{State: svc.StopPending}
				cancelar()
				<-fin
				return false, 0
			}
		case err := <-fin:
			cancelar()
			if err != nil {
				log("La vigilancia terminó con error: " + err.Error())
				return true, 1
			}
			return false, 0
		}
	}
}

// actualizarEnReposo solo actualiza cuando el centro lleva 10 min sin visores activos.
func actualizarEnReposo(ctx context.Context, mon *monitor.Monitor, rutaExe string, log func(string)) {
	esperar := 2 * time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(esperar):
		}
		esperar = 30 * time.Minute
		if mon.Activo() {
			continue
		}
		c := configActualizacion()
		c.RutaExe = rutaExe
		m, nueva, err := actualizar.Comprobar(c)
		if err != nil || !nueva {
			continue
		}
		if _, err := actualizar.Instalar(c, m); err != nil {
			log("Actualización a " + m.Version + " fallida: " + err.Error())
			continue
		}
		mon.Caja().Registrar(monitor.Evento{Fuente: "playbox", Tipo: "actualizado", Severidad: "info",
			Mensaje: fmt.Sprintf("Versión %s instalada; el servicio se reinicia", m.Version)})
		log("Actualizado a " + m.Version + "; reinicio del servicio")
		// Salida no limpia: Windows reinicia el servicio en 10 s con el exe nuevo.
		os.Exit(1)
	}
}

func registroServicio() func(string) {
	ruta := filepath.Join(dirDatos(), "servicio.log")
	_ = os.MkdirAll(dirDatos(), 0o755)
	return func(s string) {
		if info, err := os.Stat(ruta); err == nil && info.Size() > 1<<20 {
			_ = os.Rename(ruta, ruta+".1")
		}
		f, err := os.OpenFile(ruta, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		fmt.Fprintf(f, "%s  %s\n", time.Now().Format("2006-01-02 15:04:05"), s)
		f.Close()
	}
}
