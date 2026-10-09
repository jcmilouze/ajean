package ajean

import (
	"testing"
)

// triggers dit si une conversation de `used` tokens déclenche la compaction
// proactive, pour une fenêtre CTX=10000 (donc 1 % = 100 tokens).
func triggers(t *testing.T, cfg string, used int) bool {
	t.Helper()
	testHome(t)
	setConfig(t, "CTX=10000\n"+cfg)
	return compactWouldTrigger(nil, used)
}

func TestCompactAtDefaultsTo75Percent(t *testing.T) {
	if triggers(t, "", 7499) {
		t.Error("7499/10000 : pas de compaction attendue au seuil par défaut (75 %)")
	}
	if !triggers(t, "", 7500) {
		t.Error("7500/10000 : compaction attendue au seuil par défaut (75 %)")
	}
}

func TestCompactAtLowersTheThreshold(t *testing.T) {
	if triggers(t, "COMPACT_AT=60\n", 5999) {
		t.Error("5999/10000 avec COMPACT_AT=60 : trop tôt")
	}
	if !triggers(t, "COMPACT_AT=60\n", 6000) {
		t.Error("6000/10000 avec COMPACT_AT=60 : la compaction doit se déclencher")
	}
}

func TestCompactAtIsClampedAndFallsBackSafely(t *testing.T) {
	cases := []struct {
		value     string
		wantFrac  float64
		triggerAt int
	}{
		{"10", 0.30, 3000},   // trop bas : plancher à 30 %
		{"99", 0.90, 9000},   // trop haut : plafond à 90 %
		{"abc", 0.75, 7500},  // illisible : défaut
		{"", 0.75, 7500},     // vide : défaut
		{" 60 ", 0.60, 6000}, // espaces tolérés
	}
	for _, c := range cases {
		cfg := ""
		if c.value != "" {
			cfg = "COMPACT_AT=" + c.value + "\n"
		}
		if triggers(t, cfg, c.triggerAt-1) {
			t.Errorf("COMPACT_AT=%q : ne devrait pas se déclencher à %d", c.value, c.triggerAt-1)
		}
		if !triggers(t, cfg, c.triggerAt) {
			t.Errorf("COMPACT_AT=%q : devrait se déclencher à %d", c.value, c.triggerAt)
		}
	}
}

func TestCompactAtDoesNotOverrideCompactOff(t *testing.T) {
	if triggers(t, "COMPACT=off\nCOMPACT_AT=60\n", 9000) {
		t.Error("COMPACT=off doit rester prioritaire")
	}
}

func TestCompactAtSurvivesPresetSwitch(t *testing.T) {
	testHome(t)
	setConfig(t, "CTX=65000\nCOMPACT_AT=60\n")
	id, err := SavePreset("", "autre", "MODEL=a.gguf\nCTX=32768\n")
	if err != nil {
		t.Fatal(err)
	}
	p, err := safePresetPath(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPresetFile(p); err != nil {
		t.Fatal(err)
	}
	if got := ReadConfig()["COMPACT_AT"]; got != "60" {
		t.Fatalf("COMPACT_AT = %q après bascule de preset, attendu 60 (réglage utilisateur perdu)", got)
	}
}
