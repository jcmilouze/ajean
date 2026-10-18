package ajean

import "os/exec"

// jeanEmbedSysProc : rien de particulier. Sous Linux, systemd arrête le moteur
// d'embeddings avec le service (même groupe de contrôle) ; ailleurs il s'éteint
// de lui-même après sa période d'inactivité. (Pas de Pdeathsig : en Go il suit le
// thread système qui a lancé le processus, recyclé au hasard.)
func jeanEmbedSysProc(cmd *exec.Cmd) {}
