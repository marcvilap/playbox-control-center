// Package actualizar comprueba si hay una versión nueva de playbox.exe,
// la descarga, verifica su firma y sustituye el ejecutable en uso.
//
// Seguridad: el manifiesto (version.json) lleva el SHA-256 del exe y una
// firma ed25519 de "playbox|<version>|<sha256>". La clave pública va
// compilada dentro del programa: un exe sin firma válida nunca se instala.
package actualizar

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Manifiesto describe la última versión publicada.
type Manifiesto struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Firma   string `json:"firma"`
	Notas   string `json:"notas,omitempty"`
}

// MensajeFirmado es exactamente lo que se firma y se verifica.
func MensajeFirmado(version, sha string) []byte {
	return []byte("playbox|" + version + "|" + strings.ToLower(sha))
}

// Config indica dónde mirar y con qué clave verificar.
type Config struct {
	URLManifiesto string
	ClavePublica  string // hex, 32 bytes
	VersionActual string
	RutaExe       string // vacío = el ejecutable en uso
}

// MayorQue compara versiones "a.b.c" numéricamente.
func MayorQue(a, b string) bool {
	pa, pb := partes(a), partes(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func partes(v string) [3]int {
	var r [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	for i, p := range strings.SplitN(v, ".", 3) {
		n, _ := strconv.Atoi(strings.SplitN(p, "-", 2)[0])
		r[i] = n
	}
	return r
}

func urlPermitida(u string) error {
	p, err := url.Parse(u)
	if err != nil {
		return err
	}
	if p.Scheme == "https" {
		return nil
	}
	h := p.Hostname()
	if p.Scheme == "http" && (h == "localhost" || h == "127.0.0.1") {
		return nil // solo para pruebas locales
	}
	return fmt.Errorf("solo se admiten descargas https: %s", u)
}

var cliente = &http.Client{Timeout: 5 * time.Minute}

func obtener(ctx context.Context, u string, max int64) ([]byte, error) {
	if err := urlPermitida(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "playbox-actualizador")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := cliente.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s respondió %s", u, resp.Status)
	}
	datos, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(datos)) > max {
		return nil, errors.New("archivo demasiado grande")
	}
	return datos, nil
}

// Comprobar descarga el manifiesto y dice si hay una versión más nueva.
func Comprobar(c Config) (*Manifiesto, bool, error) {
	if c.URLManifiesto == "" {
		return nil, false, errors.New("sin URL de actualizaciones configurada")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	datos, err := obtener(ctx, c.URLManifiesto, 64<<10)
	if err != nil {
		return nil, false, err
	}
	var m Manifiesto
	if err := json.Unmarshal(datos, &m); err != nil {
		return nil, false, fmt.Errorf("version.json no válido: %w", err)
	}
	return &m, MayorQue(m.Version, c.VersionActual), nil
}

// Verificar comprueba hash y firma de un ejecutable descargado.
func Verificar(c Config, m *Manifiesto, exe []byte) error {
	suma := sha256.Sum256(exe)
	if !strings.EqualFold(hex.EncodeToString(suma[:]), m.SHA256) {
		return errors.New("el SHA-256 no coincide con version.json")
	}
	pub, err := hex.DecodeString(c.ClavePublica)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("clave pública mal configurada")
	}
	firma, err := base64.StdEncoding.DecodeString(m.Firma)
	if err != nil {
		return errors.New("firma ilegible")
	}
	if !ed25519.Verify(pub, MensajeFirmado(m.Version, m.SHA256), firma) {
		return errors.New("firma no válida: la actualización no procede del editor de PlayBox")
	}
	return nil
}

// Instalar descarga, verifica y sustituye el ejecutable actual.
// Devuelve la ruta del exe ya actualizado.
func Instalar(c Config, m *Manifiesto) (string, error) {
	actual := c.RutaExe
	if actual == "" {
		var err error
		if actual, err = os.Executable(); err != nil {
			return "", err
		}
		if r, err := filepath.EvalSymlinks(actual); err == nil {
			actual = r
		}
	}
	// Otro proceso (p. ej. el menú) ya dejó la versión nueva en disco.
	if enDisco, err := os.ReadFile(actual); err == nil {
		if s := sha256.Sum256(enDisco); strings.EqualFold(hex.EncodeToString(s[:]), m.SHA256) {
			return actual, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	exe, err := obtener(ctx, m.URL, 200<<20)
	if err != nil {
		return "", fmt.Errorf("descarga: %w", err)
	}
	if err := Verificar(c, m, exe); err != nil {
		return "", err
	}
	nuevo := actual + ".nuevo"
	viejo := fmt.Sprintf("%s.viejo-%d", actual, time.Now().UnixNano())
	if err := os.WriteFile(nuevo, exe, 0o755); err != nil {
		return "", fmt.Errorf("no puedo escribir en %s (¿permisos?): %w", filepath.Dir(actual), err)
	}
	// Windows permite renombrar un .exe en ejecución, pero no sobrescribirlo.
	if err := os.Rename(actual, viejo); err != nil {
		_ = os.Remove(nuevo)
		return "", fmt.Errorf("no puedo apartar la versión actual: %w", err)
	}
	if err := os.Rename(nuevo, actual); err != nil {
		_ = os.Rename(viejo, actual) // deshacer
		return "", fmt.Errorf("no puedo colocar la versión nueva: %w", err)
	}
	return actual, nil
}

// Limpiar borra los ejecutables antiguos que dejaron actualizaciones
// anteriores (los que aún estén en uso se borrarán la próxima vez).
func Limpiar() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	viejos, _ := filepath.Glob(exe + ".viejo*")
	for _, v := range viejos {
		_ = os.Remove(v)
	}
	_ = os.Remove(exe + ".nuevo")
}
