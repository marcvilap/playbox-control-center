//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"playbox/internal/ejec"
)

const claveDesinstalar = `Software\Microsoft\Windows\CurrentVersion\Uninstall\PlayBoxVR`

// Instalación por usuario: no necesita administrador y la carpeta es
// escribible, así el programa puede actualizarse solo.
func dirInstalacion() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "PlayBox")
}

func exeInstalado() string { return filepath.Join(dirInstalacion(), "playbox.exe") }

func estaInstalado() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return strings.EqualFold(filepath.Clean(exe), filepath.Clean(exeInstalado()))
}

func copiar(origen, destino string) error {
	src, err := os.Open(origen)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp := destino + ".tmp"
	dst, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	if _, err := os.Stat(destino); err == nil {
		_ = os.Rename(destino, fmt.Sprintf("%s.viejo-%d", destino, time.Now().UnixNano()))
	}
	return os.Rename(tmp, destino)
}

func accesoDirecto(lnk, destino, carpeta string) error {
	ps := fmt.Sprintf(`$s=(New-Object -ComObject WScript.Shell).CreateShortcut('%s'); $s.TargetPath='%s'; $s.WorkingDirectory='%s'; $s.Description='PlayBox VR Control Center'; $s.Save()`,
		strings.ReplaceAll(lnk, "'", "''"), strings.ReplaceAll(destino, "'", "''"), strings.ReplaceAll(carpeta, "'", "''"))
	code, out := ejec.PS(30*time.Second, ps)
	if code != 0 {
		return fmt.Errorf("acceso directo: %s", out)
	}
	return nil
}

func rutasAccesos() []string {
	escritorio, _ := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
	inicio, _ := windows.KnownFolderPath(windows.FOLDERID_Programs, 0)
	var r []string
	if escritorio != "" {
		r = append(r, filepath.Join(escritorio, "PlayBox VR.lnk"))
	}
	if inicio != "" {
		r = append(r, filepath.Join(inicio, "PlayBox VR.lnk"))
	}
	return r
}

// instalarApp copia el exe a la carpeta de usuario, crea accesos directos
// y lo registra en «Aplicaciones» de Windows. Devuelve la ruta instalada.
func instalarApp() (string, error) {
	origen, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := dirInstalacion()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	destino := exeInstalado()
	if err := copiar(origen, destino); err != nil {
		return "", fmt.Errorf("no se pudo copiar el programa: %w", err)
	}
	// Conserva la configuración de visores si ya se había hecho junto al exe descargado.
	cfgOrigen := filepath.Join(filepath.Dir(origen), "playbox.json")
	if _, err := os.Stat(filepath.Join(dir, "playbox.json")); os.IsNotExist(err) {
		if _, err := os.Stat(cfgOrigen); err == nil {
			_ = copiar(cfgOrigen, filepath.Join(dir, "playbox.json"))
		}
	}
	for _, lnk := range rutasAccesos() {
		_ = accesoDirecto(lnk, destino, dir)
	}
	if err := registrar(destino, dir); err != nil {
		return destino, fmt.Errorf("instalado, pero no se pudo registrar en Aplicaciones: %w", err)
	}
	return destino, nil
}

func registrar(exe, dir string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, claveDesinstalar, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer k.Close()
	tam := uint32(0)
	if info, err := os.Stat(exe); err == nil {
		tam = uint32(info.Size() / 1024)
	}
	for nombre, valor := range map[string]string{
		"DisplayName":     "PlayBox VR Control Center",
		"DisplayVersion":  Version,
		"Publisher":       "PlayBox VR",
		"InstallLocation": dir,
		"DisplayIcon":     exe,
		"UninstallString": fmt.Sprintf(`"%s" desinstalar-app`, exe),
	} {
		if err := k.SetStringValue(nombre, valor); err != nil {
			return err
		}
	}
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	_ = k.SetDWordValue("EstimatedSize", tam)
	return nil
}

// registrarVersionInstalada mantiene al día la versión que muestra Windows.
func registrarVersionInstalada() {
	if k, err := registry.OpenKey(registry.CURRENT_USER, claveDesinstalar, registry.SET_VALUE); err == nil {
		_ = k.SetStringValue("DisplayVersion", Version)
		k.Close()
	}
}

func cmdDesinstalarApp() int {
	r := mensaje("Desinstalar PlayBox VR",
		"Se borrará PlayBox VR Control Center de este equipo, incluidos su configuración y sus datos.\n\n¿Continuar?",
		windows.MB_YESNO|windows.MB_ICONQUESTION)
	if r != 6 { // IDYES
		return 0
	}
	for _, lnk := range rutasAccesos() {
		_ = os.Remove(lnk)
	}
	_ = registry.DeleteKey(registry.CURRENT_USER, claveDesinstalar)
	// El exe en uso no se puede borrar: lo hace un cmd en cuanto este proceso termina.
	dir := dirInstalacion()
	cmd := exec.Command("cmd.exe", "/c", fmt.Sprintf(`ping 127.0.0.1 -n 3 >nul & rmdir /s /q "%s"`, dir))
	cmd.Dir = os.TempDir()
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd.Start()
	mensaje("PlayBox VR", "PlayBox VR se ha desinstalado.", windows.MB_ICONINFORMATION)
	return 0
}
