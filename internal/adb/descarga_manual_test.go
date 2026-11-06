package adb

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Prueba real contra Google; solo con PLAYBOX_PRUEBA_DESCARGA=1.
func TestDescargarReal(t *testing.T) {
	if os.Getenv("PLAYBOX_PRUEBA_DESCARGA") == "" {
		t.Skip("prueba de red desactivada")
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := Descargar(ctx, dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"adb.exe", "AdbWinApi.dll", "AdbWinUsbApi.dll"} {
		if _, err := os.Stat(filepath.Join(dir, "platform-tools", f)); err != nil {
			t.Fatalf("falta %s", f)
		}
	}
}
