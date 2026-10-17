//go:build !windows

package ajean

import (
	"fmt"
	"os"
)

// notifyUser : hors Windows, AJEAN se lance depuis un terminal (ou via le
// service, qui journalise), donc la sortie d'erreur est visible — pas besoin
// d'une boîte de dialogue.
func notifyUser(title, text string) {
	fmt.Fprintf(os.Stderr, "%s : %s\n", title, text)
}
