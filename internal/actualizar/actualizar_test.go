package actualizar

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestMayorQue(t *testing.T) {
	casos := []struct {
		a, b string
		ok   bool
	}{
		{"0.2.1", "0.2.0", true}, {"0.10.0", "0.9.9", true}, {"1.0.0", "0.99.99", true},
		{"0.2.0", "0.2.0", false}, {"0.1.9", "0.2.0", false}, {"v0.3.0", "0.2.5", true},
	}
	for _, c := range casos {
		if MayorQue(c.a, c.b) != c.ok {
			t.Errorf("MayorQue(%s,%s) != %v", c.a, c.b, c.ok)
		}
	}
}

func TestVerificar(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	exe := []byte("binario de prueba")
	s := sha256.Sum256(exe)
	sha := hex.EncodeToString(s[:])
	m := &Manifiesto{Version: "0.2.1", SHA256: sha,
		Firma: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, MensajeFirmado("0.2.1", sha)))}
	c := Config{ClavePublica: hex.EncodeToString(pub)}
	if err := Verificar(c, m, exe); err != nil {
		t.Fatal("debería aceptar:", err)
	}
	if Verificar(c, m, []byte("manipulado")) == nil {
		t.Fatal("debería rechazar un exe distinto")
	}
	m2 := *m
	m2.Version = "9.9.9" // firma de otra versión
	if Verificar(c, &m2, exe) == nil {
		t.Fatal("debería rechazar versión no firmada")
	}
	otra, _, _ := ed25519.GenerateKey(rand.Reader)
	if Verificar(Config{ClavePublica: hex.EncodeToString(otra)}, m, exe) == nil {
		t.Fatal("debería rechazar otra clave")
	}
}

func TestURLPermitida(t *testing.T) {
	if urlPermitida("http://ejemplo.com/x") == nil {
		t.Fatal("http externo debe rechazarse")
	}
	if urlPermitida("https://ejemplo.com/x") != nil || urlPermitida("http://127.0.0.1:8000/x") != nil {
		t.Fatal("https y localhost deben admitirse")
	}
}
