package ajean

import (
	"strings"
	"testing"
)

// Partage des couches (#121) : la clé « gpu » liste les deux cartes (le serveur
// du moteur en déduit --layer-split), et rien du mode d'aide ne subsiste.
func TestMoeBuildConfigLayerSplit(t *testing.T) {
	base := map[string]any{
		"args":   []any{"--pack", "/p", "--vram-reserve-mib", "900"},
		"vision": map[string]any{"exe": "/v"},
	}
	cfg := map[string]string{"MOE_MAIN_GPU": "1", "MOE_HELPER_GPU": "0", "MOE_SPLIT": "layers", "MOE_DROP": "1"}
	out, err := moeBuildConfig(base, cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	g, ok := out["gpu"].([]any)
	if !ok || len(g) != 2 || g[0] != 1 || g[1] != 0 {
		t.Fatalf("gpu = %v, attendu [1 0] (principale d'abord, numéros nvidia-smi)", out["gpu"])
	}
	a := moeArgs(t, out)
	if hasArg(a, "--remote-expert-opt") || hasArg(a, "--expert-cache-device1") {
		t.Errorf("options du mode d'aide en partage des couches : %v", a)
	}
	if argAfter(a, "--vram-reserve-mib") != "900" {
		t.Errorf("la réserve de VRAM de l'installeur doit rester : %v", a)
	}
	if hasArg(a, "--prefill") {
		t.Errorf("--prefill forcé en partage des couches (mesuré défavorable, #129) : %v", a)
	}
	env := out["env"].(map[string]any)
	if env["STRATA_REMOTE_DROP"] != nil {
		t.Errorf("experts hors RAM (mode d'aide) activé en partage des couches : %v", env)
	}
	if _, has := out["vision"].(map[string]any)["cuda_device"]; has {
		t.Errorf("encodeur d'images déplacé sur la seconde carte en partage des couches")
	}

	// Sans la clé, rien ne change : le mode d'aide reste le défaut.
	delete(cfg, "MOE_SPLIT")
	out, _ = moeBuildConfig(base, cfg, "")
	if _, has := out["gpu"]; has {
		t.Errorf("clé gpu posée en mode d'aide : %v", out["gpu"])
	}
	if !hasArg(moeArgs(t, out), "--remote-expert-opt") {
		t.Errorf("mode d'aide perdu sans MOE_SPLIT")
	}
	if argAfter(moeArgs(t, out), "--prefill") != "16384" {
		t.Errorf("--prefill 16384 attendu en mode d'aide : %v", moeArgs(t, out))
	}
	// Seconde carte coupée : pas de partage même si la clé traîne.
	cfg["MOE_SPLIT"], cfg["MOE_HELPER"] = "layers", "0"
	if moeLayerSplit(cfg) {
		t.Errorf("partage des couches avec la seconde carte coupée")
	}
}

func TestMoeApplySettingsSplit(t *testing.T) {
	in := "ENGINE=moe\nMOE_HELPER_GPU=0\n"
	req := moeSettingsReq{Ctx: 131072, KV: "fp16", Spec: 3, Vision: true, Helper: true, Split: "layers"}
	out, err := moeApplySettings(in, req)
	if err != nil || !strings.Contains(out, "MOE_SPLIT=layers\n") {
		t.Fatalf("MOE_SPLIT=layers attendu : %q (%v)", out, err)
	}
	req.Split = "helper"
	if out, _ = moeApplySettings(out, req); strings.Contains(out, "MOE_SPLIT") {
		t.Errorf("retour au mode d'aide : la clé doit disparaître : %q", out)
	}
	// un ancien client n'envoie pas « split » : la clé est conservée
	req.Split = ""
	out, _ = moeApplySettings("MOE_SPLIT=layers\n", req)
	if !strings.Contains(out, "MOE_SPLIT=layers") {
		t.Errorf("clé effacée par un client qui ne la connaît pas : %q", out)
	}
	req.Split = "nimporte"
	if _, err := moeApplySettings(in, req); err == nil {
		t.Errorf("répartition inconnue acceptée")
	}
}

// Point de coupure fixe (#2418, #2463 : l'auto de la 0.1.44.1 déséquilibre une
// 4090 + 3090) et réglages libres du moteur posés par le preset.
func TestMoeBuildConfigLayerSplitK(t *testing.T) {
	base := map[string]any{"args": []any{"--pack", "/p", "--pipeline-windows", "1"}}
	cfg := map[string]string{"MOE_MAIN_GPU": "1", "MOE_HELPER_GPU": "0", "MOE_SPLIT": "layers", "MOE_LAYER_SPLIT": "27",
		"MOE_ENGINE_ENV": "STRATA_SPLIT_LINK_ABS=1  STRATA_PREFILL_HELP=1", "MOE_ENGINE_ARGS": "--trim-stage-weights --pipeline-windows 2"}
	out, err := moeBuildConfig(base, cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if out["layer_split"] != "27" || out["gpu_order"] != "as_given" {
		t.Errorf("layer_split=%v gpu_order=%v, attendu 27 / as_given", out["layer_split"], out["gpu_order"])
	}
	a := moeArgs(t, out)
	if !hasArg(a, "--trim-stage-weights") || argAfter(a, "--pipeline-windows") != "2" {
		t.Errorf("MOE_ENGINE_ARGS mal appliqué : %v", a)
	}
	env := out["env"].(map[string]any)
	if env["STRATA_SPLIT_LINK_ABS"] != "1" || env["STRATA_PREFILL_HELP"] != "1" {
		t.Errorf("MOE_ENGINE_ENV mal appliqué : %v", env)
	}

	// K sans partage des couches : ignoré (le mode d'aide n'a pas de couches à couper).
	delete(cfg, "MOE_SPLIT")
	if out, _ = moeBuildConfig(base, cfg, ""); out["layer_split"] != nil {
		t.Errorf("layer_split posé hors partage des couches : %v", out["layer_split"])
	}
}
