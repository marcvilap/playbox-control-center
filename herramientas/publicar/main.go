// publicar genera la clave de firma y el version.json de cada versión.
//
//	go run ./herramientas/publicar -generar-clave -clave clave-firma-playbox.txt
//	go run ./herramientas/publicar -clave clave-firma-playbox.txt \
//	    -exe dist/playbox.exe -version 0.2.1 \
//	    -url https://mhou.es/playbox/playbox-0.2.1.exe -notas "Qué cambia" \
//	    -salida dist/version.json
//
// La clave privada NUNCA se sube al servidor ni al repositorio.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"playbox/internal/actualizar"
)

func main() {
	generar := flag.Bool("generar-clave", false, "crea un par de claves nuevo")
	rutaClave := flag.String("clave", "clave-firma-playbox.txt", "archivo con la clave privada (hex)")
	exe := flag.String("exe", "dist/playbox.exe", "ejecutable a publicar")
	version := flag.String("version", "", "versión del exe, p. ej. 0.2.1")
	url := flag.String("url", "", "URL https pública donde estará el exe")
	notas := flag.String("notas", "", "qué cambia en esta versión")
	salida := flag.String("salida", "dist/version.json", "dónde escribir el manifiesto")
	flag.Parse()

	if *generar {
		if _, err := os.Stat(*rutaClave); err == nil {
			fallar("ya existe %s; no la sobrescribo", *rutaClave)
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		comprobar(err)
		comprobar(os.WriteFile(*rutaClave, []byte(hex.EncodeToString(priv.Seed())+"\n"), 0o600))
		fmt.Println("Clave privada guardada en", *rutaClave, "— guárdala fuera del servidor.")
		fmt.Println("Clave pública (va compilada en el exe):", hex.EncodeToString(pub))
		return
	}
	if *version == "" || !strings.HasPrefix(*url, "https://") {
		fallar("faltan -version o -url https://…")
	}
	semilla, err := os.ReadFile(*rutaClave)
	comprobar(err)
	s, err := hex.DecodeString(strings.TrimSpace(string(semilla)))
	if err != nil || len(s) != ed25519.SeedSize {
		fallar("clave privada no válida en %s", *rutaClave)
	}
	priv := ed25519.NewKeyFromSeed(s)
	datos, err := os.ReadFile(*exe)
	comprobar(err)
	suma := sha256.Sum256(datos)
	sha := hex.EncodeToString(suma[:])
	m := actualizar.Manifiesto{
		Version: *version, URL: *url, SHA256: sha, Notas: *notas,
		Firma: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, actualizar.MensajeFirmado(*version, sha))),
	}
	j, _ := json.MarshalIndent(m, "", "  ")
	comprobar(os.WriteFile(*salida, append(j, '\n'), 0o644))
	fmt.Printf("version.json escrito en %s (versión %s, sha256 %s…)\n", *salida, *version, sha[:12])
	fmt.Println("Sube al servidor, en este orden: 1) el exe en", *url, " 2) version.json")
}

func comprobar(err error) {
	if err != nil {
		fallar("%v", err)
	}
}

func fallar(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+f+"\n", a...)
	os.Exit(1)
}
