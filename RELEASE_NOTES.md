Recherche par le sens dans le journal de Jean, retour aux outils séparés et nettoyage du code.

## Nouveautés

- **Recherche par le sens dans le journal de Jean** : Jean retrouve un échange passé d'après ce qu'il veut dire, et plus seulement d'après les mots exacts. Sur un journal réel, la bonne entrée figure dans les cinq premiers résultats 9 fois sur 10, contre 4 fois sur 10 avec la recherche par mots. Un petit modèle d'embeddings (environ 330 Mo, téléchargé à l'activation) tourne sur le processeur, sans prendre de VRAM, et s'arrête après 20 minutes sans usage. L'index est calculé quand Jean ne génère pas, et il est chiffré si la mémoire l'est. L'option s'active dans le panneau de mémoire de Jean. La recherche par mots reste utilisée en secours.

## Corrections

- **Outils mal appelés depuis la 0.18.2** : le regroupement des outils par domaine faisait se tromper les modèles locaux sur les paramètres (environ 6 % d'appels mal formés, contre 0,2 % avant). Les outils redeviennent séparés, comme en 0.18.1. Les conversations de la période regroupée restent lisibles.
- **Fiches de Jean** : un nom de fiche passé sous « fiche » au lieu de « name » est maintenant accepté.
- **Première recherche après une pause** : la recherche attend le démarrage du modèle d'embeddings (environ 2 secondes) au lieu de repasser par la recherche par mots.
- **Recherche par mots avec une phrase** : les mots courts et les mots vides ne ramènent plus tout le journal.

## Interne

- Code inutilisé retiré, sans changement de comportement.

## Mise à jour

```
ajean update
```
