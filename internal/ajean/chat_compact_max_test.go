package ajean

import "testing"

// Plafond du seuil de compactage (#142) : sans réglage, 75 % de la fenêtre ;
// avec, le plus bas des deux ; un plafond minuscule est relevé.
func TestCompactThresholdCap(t *testing.T) {
	testHome(t)
	set := func(k, v string) {
		t.Helper()
		if err := SetConfigKey(k, v); err != nil {
			t.Fatal(err)
		}
	}
	set("CTX", "1000000")
	set("COMPACT_MAX", "")
	if got := compactThreshold(); got != 750000 {
		t.Fatalf("sans plafond : %d, attendu 750000", got)
	}
	set("COMPACT_MAX", "32768")
	if got := compactThreshold(); got != 32768 {
		t.Fatalf("plafond 32768 : %d", got)
	}
	// fenêtre plus petite que le plafond : la fenêtre décide
	set("CTX", "32768")
	if got := compactThreshold(); got != 24576 {
		t.Fatalf("petite fenêtre : %d, attendu 24576", got)
	}
	set("CTX", "1000000")
	set("COMPACT_MAX", "100")
	if got := compactThreshold(); got != 8192 {
		t.Fatalf("plafond minuscule non relevé : %d", got)
	}
	// le seuil plafonné déclenche bien la compaction
	set("COMPACT_MAX", "32768")
	if !compactWouldTrigger(nil, 40000) || compactWouldTrigger(nil, 20000) {
		t.Fatalf("déclenchement incohérent avec le plafond")
	}
}

// COMPACT_MAX est un réglage de l'appareil : il survit à un changement de preset.
func TestCompactMaxPreserved(t *testing.T) {
	if !isPreservedKey("COMPACT_MAX") {
		t.Fatal("COMPACT_MAX serait effacé par un changement de preset")
	}
}
