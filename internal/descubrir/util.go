package descubrir

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"playbox/internal/ejec"
)

// ComoLista interpreta la salida JSON de ConvertTo-Json (objeto o lista).
func ComoLista(txt string) []map[string]any {
	txt = strings.TrimPrefix(ejec.Stdout(txt), "\ufeff")
	if txt == "" {
		return nil
	}
	var v any
	if json.Unmarshal([]byte(txt), &v) != nil {
		return nil
	}
	switch t := v.(type) {
	case map[string]any:
		return []map[string]any{t}
	case []any:
		var out []map[string]any
		for _, e := range t {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// ComoTextos interpreta una salida JSON que es una cadena o lista de cadenas.
func ComoTextos(txt string) []string {
	txt = strings.TrimPrefix(ejec.Stdout(txt), "\ufeff")
	var v any
	if txt == "" || json.Unmarshal([]byte(txt), &v) != nil {
		return nil
	}
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// S convierte un valor JSON en texto legible.
func S(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

// Buscar devuelve el primer grupo capturado de la expresión, o "".
func Buscar(re *regexp.Regexp, txt string) string {
	if m := re.FindStringSubmatch(txt); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func guardar(dir, nombre, contenido string) {
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, nombre+".txt"), []byte(contenido), 0o644)
}

var noSeguro = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func nombreSeguro(s string) string { return noSeguro.ReplaceAllString(s, "_") }
