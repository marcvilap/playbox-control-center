package pc

import "testing"

func TestNvidiaSMI(t *testing.T) {
	g := ParsearNvidiaSMI("NVIDIA GeForce RTX 4070, 37, 61, 3120, 12282\n")
	if g == nil || g.Nombre != "NVIDIA GeForce RTX 4070" || g.UsoPct != 37 || g.TempC != 61 || g.VRAMTotMB != 12282 {
		t.Fatalf("%+v", g)
	}
	if ParsearNvidiaSMI("") != nil {
		t.Fatal("vacío debe dar nil")
	}
}

func TestEntrada(t *testing.T) {
	e := ParsearEntrada(`[{"Status":"OK","Class":"Keyboard","FriendlyName":"Teclado HID"},
	{"Status":"Error","Class":"Mouse","FriendlyName":"Mouse compatible con HID"},
	{"Status":"OK","Class":"Mouse","FriendlyName":"Logitech"}]`)
	if e.Teclados != 1 || e.Ratones != 2 || len(e.Problemas) != 1 {
		t.Fatalf("%+v", e)
	}
}

func TestLeer(t *testing.T) {
	Leer()
	s := Leer()
	if s.RAMTotalGB <= 0 || s.DiscoPct <= 0 {
		t.Fatalf("lectura vacía: %+v", s)
	}
}
