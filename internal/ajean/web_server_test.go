package ajean

import "testing"

// pidFromNetstat doit lire l'état d'écoute SANS dépendre de la langue de Windows.
// Le filtre d'origine cherchait « LISTENING » en dur et ne matchait donc jamais
// sur un poste non anglophone : le processus qui occupe le port restait
// introuvable et l'utilisateur recevait « le process n'a pas pu être identifié ».
// Les sorties ci-dessous sont celles que produisent réellement les systèmes.
func TestPidFromNetstat(t *testing.T) {
	cases := []struct {
		name string
		out  string
		port int
		want int
	}{
		{
			name: "anglais",
			out:  "  TCP    0.0.0.0:8090           0.0.0.0:0              LISTENING       36316\n",
			port: 8090, want: 36316,
		},
		{
			name: "allemand — ce que rend ce poste",
			out:  "  TCP    0.0.0.0:8090           0.0.0.0:0              ABHÖREN         36316\n",
			port: 8090, want: 36316,
		},
		{
			name: "français",
			out:  "  TCP    0.0.0.0:8090           0.0.0.0:0              ÉCOUTE          36316\n",
			port: 8090, want: 36316,
		},
		{
			name: "IPv6",
			out:  "  TCP    [::]:8090              [::]:0                 LISTENING       42\n",
			port: 8090, want: 42,
		},
		{
			name: "une connexion établie n'est pas une écoute",
			out:  "  TCP    127.0.0.1:25339        127.0.0.1:8090         WARTEND         0\n",
			port: 8090, want: 0,
		},
		{
			name: "un autre port ne compte pas",
			out:  "  TCP    0.0.0.0:8080           0.0.0.0:0              ABHÖREN         999\n",
			port: 8090, want: 0,
		},
		{
			name: "un port qui finit par les mêmes chiffres ne compte pas",
			out:  "  TCP    0.0.0.0:18090          0.0.0.0:0              ABHÖREN         999\n",
			port: 8090, want: 0,
		},
		{
			name: "plusieurs lignes, la bonne au milieu",
			out: "  TCP    0.0.0.0:8080           0.0.0.0:0              ABHÖREN         999\n" +
				"  TCP    0.0.0.0:8090           0.0.0.0:0              ABHÖREN         555\n" +
				"  TCP    0.0.0.0:9000           0.0.0.0:0              ABHÖREN         777\n",
			port: 8090, want: 555,
		},
		{name: "sortie vide", out: "", port: 8090, want: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pidFromNetstat(c.out, c.port); got != c.want {
				t.Fatalf("attendu %d, obtenu %d", c.want, got)
			}
		})
	}
}
