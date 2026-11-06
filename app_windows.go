//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

var mutexInstancia windows.Handle

func mensaje(titulo, texto string, flags uint32) int {
	t, _ := windows.UTF16PtrFromString(titulo)
	m, _ := windows.UTF16PtrFromString(texto)
	r, _ := windows.MessageBox(0, m, t, flags)
	return int(r)
}

// instanciaUnica evita abrir dos ventanas a la vez.
func instanciaUnica() bool {
	nombre, _ := windows.UTF16PtrFromString(`Local\PlayBoxControlCenter`)
	h, err := windows.CreateMutex(nil, false, nombre)
	if err == windows.ERROR_ALREADY_EXISTS {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return false
	}
	mutexInstancia = h
	return true
}

func liberarInstanciaUnica() {
	if mutexInstancia != 0 {
		windows.CloseHandle(mutexInstancia)
		mutexInstancia = 0
	}
}

func runApp() int {
	if !instanciaUnica() {
		mensaje("PlayBox VR", "PlayBox ya está abierto.", windows.MB_ICONINFORMATION)
		return 0
	}
	defer liberarInstanciaUnica()
	if estaInstalado() {
		registrarVersionInstalada()
	}
	a, url, err := iniciarApp()
	if err != nil {
		mensaje("PlayBox VR", "No se pudo iniciar: "+err.Error(), windows.MB_ICONERROR)
		return 1
	}
	datosWeb := filepath.Join(os.Getenv("LOCALAPPDATA"), "PlayBox", "webview")
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  datosWeb,
		WindowOptions: webview2.WindowOptions{
			Title: "PlayBox VR Control Center", Width: 1180, Height: 820, Center: true,
		},
	})
	if w == nil {
		// Sin WebView2: se abre en el navegador y se cierra con el botón «Salir».
		fin := make(chan struct{})
		a.salir = func() { close(fin) }
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		<-fin
		a.pararMonitor()
		return 0
	}
	defer w.Destroy()
	a.salir = func() { w.Dispatch(func() { w.Terminate() }) }
	w.SetSize(1180, 820, webview2.HintNone)
	w.Navigate(url)
	w.Run()
	a.pararMonitor()
	return 0
}

var _ = unsafe.Pointer(nil)
