package ajean

import (
	"strings"
	"testing"
)

// VRAM laissée à l'écran (#147) : écrite dans le preset, retirée à 0, bornée,
// et transmise au moteur même en mode d'aide (qui retire la réserve de
// l'installeur).
func TestMoeDisplayReserve(t *testing.T) {
	req := moeSettingsReq{Ctx: 131072, KV: "fp16", Spec: 3, Vision: true, Helper: true}
	n := 3072
	req.Reserve = &n
	out, err := moeApplySettings("ENGINE=moe\nMOE_HELPER_GPU=0\n", req)
	if err != nil || !strings.Contains(out, "MOE_VRAM_RESERVE=3072\n") {
		t.Fatalf("réserve non écrite : %q (%v)", out, err)
	}
	// un ancien client n'envoie pas la réserve : conservée
	req.Reserve = nil
	if kept, _ := moeApplySettings(out, req); !strings.Contains(kept, "MOE_VRAM_RESERVE=3072") {
		t.Errorf("réserve effacée par un client qui ne la connaît pas : %q", kept)
	}
	zero := 0
	req.Reserve = &zero
	if cleared, _ := moeApplySettings(out, req); strings.Contains(cleared, "MOE_VRAM_RESERVE") {
		t.Errorf("réserve à 0 : la clé doit disparaître : %q", cleared)
	}
	big := moeMaxReserveMiB + 1
	req.Reserve = &big
	if _, err := moeApplySettings(out, req); err == nil {
		t.Errorf("réserve hors limites acceptée")
	}

	base := map[string]any{"args": []any{"--pack", "/p", "--vram-reserve-mib", "700"}}
	cfg := map[string]string{"MOE_HELPER_GPU": "0", "MOE_VRAM_RESERVE": "3072"}
	built, err := moeBuildConfig(base, cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if v := argAfter(moeArgs(t, built), "--vram-reserve-mib"); v != "3072" {
		t.Errorf("mode d'aide : réserve de l'écran attendue à 3072, obtenue %q", v)
	}
	// sans réglage, le mode d'aide garde son comportement (réserve de l'installeur retirée)
	delete(cfg, "MOE_VRAM_RESERVE")
	built, _ = moeBuildConfig(base, cfg, "")
	if hasArg(moeArgs(t, built), "--vram-reserve-mib") {
		t.Errorf("réserve posée sans réglage en mode d'aide : %v", moeArgs(t, built))
	}
}
