package ajean

import "testing"

// La jauge et le compactage voient le contexte que le moteur applique : en
// mode hors RAM, le moteur MoE le ramène à moeDropMaxCtx.
func TestCtxWindowMoeDropCap(t *testing.T) {
	testHome(t)
	set := func(k, v string) {
		t.Helper()
		if err := SetConfigKey(k, v); err != nil {
			t.Fatal(err)
		}
	}
	set("ENGINE", "moe")
	set("MOE_HELPER_GPU", "0")
	set("MOE_DROP", "1")
	set("CTX", "167936")
	if got := ctxWindow(); got != moeDropMaxCtx {
		t.Fatalf("hors RAM : %d, attendu %d", got, moeDropMaxCtx)
	}
	set("MOE_DROP", "0")
	if got := ctxWindow(); got != 167936 {
		t.Fatalf("sans mode hors RAM : %d, attendu 167936", got)
	}
	set("MOE_DROP", "1")
	set("ENGINE", "")
	if got := ctxWindow(); got != 167936 {
		t.Fatalf("llama.cpp : le CTX du preset doit rester : %d", got)
	}
}
