//go:build windows

package ajean

// notifyUser montre un message à l'utilisateur.
//
// Nécessaire parce qu'en mode application — double-clic, aucun argument — il n'y
// a AUCUNE console : un Printf n'est vu de personne et le programme semble ne
// rien faire du tout. C'est exactement ce qui se passait quand le port 8090 était
// tenu par un autre logiciel.
func notifyUser(title, text string) {
	messageBox(text, title, mbIconInfo)
}
