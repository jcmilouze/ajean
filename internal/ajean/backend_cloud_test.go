package ajean

import (
	"strings"
	"testing"
)

func TestCloudServerArgs(t *testing.T) {
	cfg := map[string]string{
		"CLOUD":       "modal",
		"CLOUD_MODEL": "https://huggingface.co/x/y/blob/main/Model-Q4_K_M.gguf?download=true",
		"CTX":         "65536",
		"KV_TYPE":     "q8_0",
		"REASONING":   "off",
		"EXTRA_ARGS":  `--device CUDA0 --flash-attn on --mlock --no-mmap -ts 0.9,0.1 --spec-type draft-mtp --chat-template-file "/etc/x y.jinja" --jinja`,
	}
	args, err := cloudServerArgs(cfg, "k")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	for _, want := range []string{"-m /models/Model-Q4_K_M.gguf", "-c 65536", "-ctk q8_0", "-ctv q8_0",
		"--reasoning off", "--api-key k", "--flash-attn on", "--spec-type draft-mtp", "--jinja", "--load-mode mlock"} {
		if !strings.Contains(got, want) {
			t.Errorf("manque %q dans %q", want, got)
		}
	}
	// Réglages propres à la machine locale : jamais envoyés au conteneur.
	for _, bad := range []string{"--device", "CUDA0", "-ts", "--chat-template-file", "--mlock", "--no-mmap"} {
		for _, a := range args {
			if a == bad {
				t.Errorf("%q ne doit pas partir vers Modal : %q", bad, got)
			}
		}
	}
}

func TestCloudServerArgsRejectsNonGGUF(t *testing.T) {
	if _, err := cloudServerArgs(map[string]string{"CLOUD_MODEL": "https://huggingface.co/x/y"}, ""); err == nil {
		t.Fatal("un lien sans .gguf doit être refusé")
	}
}

func TestCloudModelURL(t *testing.T) {
	if got := cloudModelURL("https://huggingface.co/a/b/blob/main/m.gguf"); got != "https://huggingface.co/a/b/resolve/main/m.gguf" {
		t.Fatal(got)
	}
}

func TestCloudDraftAndOutputBudget(t *testing.T) {
	cfg := map[string]string{
		"CLOUD":           "modal",
		"CLOUD_MODEL":     "https://huggingface.co/example/target/resolve/main/target.gguf",
		"CLOUD_DRAFT":     "https://huggingface.co/example/draft/blob/main/draft-Q4_K_M.gguf",
		"CLOUD_N_PREDICT": "4800",
		"EXTRA_ARGS":      "--flash-attn on --spec-type draft-dflash --spec-draft-n-max 5 --model-draft C:/local/draft.gguf --jinja",
	}
	args, err := cloudServerArgs(cfg, "secret")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--model-draft /models/draft-Q4_K_M.gguf", "--n-predict 4800",
		"--spec-type draft-dflash", "--spec-draft-n-max 5",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "C:/local/draft.gguf") {
		t.Fatalf("local draft path must not reach Modal: %s", joined)
	}
	files := cloudFiles(cfg)
	if len(files) != 2 || files[1]["file"] != "draft-Q4_K_M.gguf" ||
		files[1]["url"] != "https://huggingface.co/example/draft/resolve/main/draft-Q4_K_M.gguf" {
		t.Fatalf("target and draft must be downloaded to the Modal volume: %+v", files)
	}
	oldFP := cloudFingerprint(cfg)
	cfg["CLOUD_DRAFT"] = "https://huggingface.co/example/draft/resolve/main/draft-Q8_0.gguf"
	if cloudFingerprint(cfg) == oldFP {
		t.Fatal("changing the draft must redeploy")
	}
	cfg["CLOUD_DRAFT"] = "https://huggingface.co/example/draft/resolve/main/draft-Q4_K_M.gguf"
	oldFP = cloudFingerprint(cfg)
	cfg["CLOUD_N_PREDICT"] = "6000"
	if cloudFingerprint(cfg) == oldFP {
		t.Fatal("changing the output budget must redeploy")
	}
}

func TestCloudDraftAndPredictInvalid(t *testing.T) {
	cfg := map[string]string{
		"CLOUD_MODEL": "https://huggingface.co/example/resolve/main/target.gguf",
		"CLOUD_DRAFT": "https://huggingface.co/example/draft",
	}
	if _, err := cloudServerArgs(cfg, ""); err == nil {
		t.Fatal("draft without .gguf must be rejected")
	}
	cfg["CLOUD_DRAFT"] = "https://huggingface.co/example/draft.gguf"
	for _, invalid := range []string{"0", "-1", "nan", "4.8k"} {
		cfg["CLOUD_N_PREDICT"] = invalid
		if _, err := cloudServerArgs(cfg, ""); err == nil {
			t.Errorf("invalid output budget %q accepted", invalid)
		}
	}
}

// Un preset sans draft ni budget garde l'empreinte d'avant ces options : pas de
// redéploiement des presets existants à la mise à jour.
func TestCloudFingerprintStableWithoutDraft(t *testing.T) {
	cfg := map[string]string{
		"CLOUD":       "modal",
		"CLOUD_MODEL": "https://huggingface.co/example/target/resolve/main/target.gguf",
	}
	args, err := cloudServerArgs(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	want := configFingerprint(map[string]string{
		"a": strings.Join(args, "\x00"), "g": cloudGPU(cfg), "m": cfg["CLOUD_MODEL"],
		"v": "", "i": cloudIdle(cfg), "p": "",
	})
	if got := cloudFingerprint(cfg); got != want {
		t.Fatalf("empreinte modifiée pour un preset sans draft : %s != %s", got, want)
	}
}

func TestCloudDraftSameFileRejected(t *testing.T) {
	cfg := map[string]string{
		"CLOUD_MODEL": "https://huggingface.co/example/a/resolve/main/model.gguf",
		"CLOUD_DRAFT": "https://huggingface.co/example/b/resolve/main/model.gguf",
	}
	if _, err := cloudServerArgs(cfg, ""); err == nil {
		t.Fatal("un draft du même nom que le modèle doit être refusé")
	}
}

func TestUsesRemoteEndpoint(t *testing.T) {
	if !usesRemoteEndpoint(map[string]string{"CLOUD": "modal"}) || !usesRemoteEndpoint(map[string]string{"EXTERNAL": "1"}) {
		t.Fatal("cloud et externe n'ont pas de moteur local")
	}
	if usesRemoteEndpoint(map[string]string{"MODEL": "x.gguf"}) {
		t.Fatal("un preset local a un moteur local")
	}
}

// Les erreurs encadrées de la CLI Modal deviennent une phrase courte et utile.
func TestModalErrorText(t *testing.T) {
	spend := "┌─ Error ──────┐\n│ Workspace ac-6uRQ has exceeded its spend limit │\n└──────────────┘"
	if got := modalErrorText(spend); !strings.Contains(got, "limite de dépense") {
		t.Errorf("limite : %q", got)
	}
	prof := "│ Modal profile 'test' was not found in /home/x/.modal.toml. │"
	if got := modalErrorText(prof); !strings.Contains(got, "introuvable") {
		t.Errorf("profil : %q", got)
	}
	if got := modalErrorText("│ something odd happened │"); got != "something odd happened" {
		t.Errorf("brut : %q", got)
	}
}

func TestModalDeployURL(t *testing.T) {
	const want = "https://testaccount--ajean-coder390-modal-l40s-8k-validation-serve.modal.run"
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "normal deployment",
			output: "✓ Created Function serve\n    └── " + want + " (unauthenticated)\n✓ App deployed!\n",
			want:   want,
		},
		{
			name:   "hard wrapped hostname",
			output: "✓ Created Function serve\n    └── https://testaccount--ajean-coder390-modal-l40s-8k-validation-serve.moda\n        l.run (unauthenticated)\n✓ App deployed!\n",
			want:   want,
		},
		{
			name:   "wrapped even earlier",
			output: "    └── https://testaccount--ajean-coder390-modal-\r\n        l40s-8k-validation-serve.mo\r\n        dal.run (unauthenticated)\r\n✓ Deployment skipped: no changes detected!\r\n",
			want:   want,
		},
		{
			name:   "unrelated URL is not an endpoint",
			output: "View Deployment: https://modal.com/apps/testaccount/main/deployed/example\n",
		},
		{
			name:   "missing endpoint",
			output: "Error: deployment failed\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modalDeployURL(tt.output); got != tt.want {
				t.Fatalf("modalDeployURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Vision : le projecteur est téléchargé à côté du modèle et passé à llama-server.
func TestCloudVision(t *testing.T) {
	cfg := map[string]string{
		"CLOUD":        "modal",
		"CLOUD_MODEL":  "https://huggingface.co/a/b/resolve/main/m.gguf",
		"CLOUD_MMPROJ": "https://huggingface.co/a/b/blob/main/mmproj-F16.gguf",
	}
	args, err := cloudServerArgs(cfg, "")
	if err != nil || !strings.Contains(strings.Join(args, " "), "--mmproj /models/mmproj-F16.gguf") {
		t.Fatalf("args = %v, err = %v", args, err)
	}
	f := cloudFiles(cfg)
	if len(f) != 2 || f[1]["url"] != "https://huggingface.co/a/b/resolve/main/mmproj-F16.gguf" {
		t.Fatalf("fichiers = %v", f)
	}
	cfg["CLOUD_MMPROJ"] = "https://huggingface.co/a/b"
	if _, err := cloudServerArgs(cfg, ""); err == nil {
		t.Fatal("un lien de projecteur sans .gguf doit être refusé")
	}
}

// Un bench n'est affiché que pour le modèle sur lequel il a été mesuré.
func TestBenchMatchesPreset(t *testing.T) {
	sb := savedBench{Model: "Swift-IQ4_XS.gguf"}
	if !benchMatchesPreset(sb, map[string]string{"MODEL": "/etc/ajean/models/Swift-IQ4_XS.gguf"}) {
		t.Fatal("même modèle : bench attendu")
	}
	if benchMatchesPreset(sb, map[string]string{"MODEL": "Autre.gguf"}) {
		t.Fatal("autre modèle : pas de bench")
	}
	if benchMatchesPreset(sb, map[string]string{"CLOUD": "modal", "MODEL": "Swift-IQ4_XS.gguf"}) {
		t.Fatal("preset distant : pas de bench")
	}
}
