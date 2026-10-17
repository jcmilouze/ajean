package ajean

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Après un redémarrage, une mémoire modifiée depuis la dernière consolidation
// doit suffire à la programmer, sans échange avec Jean.
func TestJeanConsolidateArmAtStart(t *testing.T) {
	testHome(t)
	_ = os.MkdirAll(jeanDir(), 0o700)
	stop := func() {
		jeanReflect.mu.Lock()
		if jeanConsolidateTimer.t != nil {
			jeanConsolidateTimer.t.Stop()
			jeanConsolidateTimer.t = nil
		}
		jeanReflect.mu.Unlock()
	}
	defer stop()
	stop()

	// Rien de modifié : rien de programmé.
	jeanMarkConsolidated()
	jeanConsolidateArmAtStart()
	if jeanConsolidateTimer.t != nil {
		t.Fatal("consolidation programmée alors que la mémoire n'a pas bougé")
	}

	// Profil modifié après la dernière consolidation : programmée.
	p := filepath.Join(jeanDir(), jeanProfileFile)
	if err := os.WriteFile(p, []byte("- aime le thé\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(p, later, later)
	jeanConsolidateArmAtStart()
	if jeanConsolidateTimer.t == nil {
		t.Fatal("consolidation non programmée après un redémarrage")
	}
}

// Sans tour Jean depuis le démarrage, la consolidation garde les outils de Jean.
func TestJeanTurnCapsDefault(t *testing.T) {
	jeanReflect.mu.Lock()
	saved, savedT := jeanReflect.caps, jeanReflect.temp
	jeanReflect.caps, jeanReflect.temp = Caps{}, 0
	caps, temp := jeanTurnCapsLocked()
	jeanReflect.caps, jeanReflect.temp = saved, savedT
	jeanReflect.mu.Unlock()
	if !caps.Jean || caps.Mem != MemOff || temp != 0.7 {
		t.Fatalf("réglages par défaut inattendus : jean=%v mem=%v temp=%v", caps.Jean, caps.Mem, temp)
	}
}
