Historique des réponses des tâches, choix du navigateur des liens, nouveaux réglages du compactage et du moteur MoE, et de nombreuses corrections.

## Nouveautés

- **Historique des tâches** : chaque tâche garde ses 20 derniers passages (date, durée, issue et réponse complète). Ils s'affichent sous le dernier résultat, et chaque passage se déplie pour relire sa réponse.
- **Navigateur des liens** (Paramètres, Général) : sous Windows, un lien cliqué s'ouvrait toujours dans Edge, même avec Chrome comme navigateur par défaut. Par défaut, les liens suivent désormais le navigateur du système ; Chrome, Edge, Brave ou Chromium peuvent aussi être choisis. La fenêtre d'AJEAN ne change pas, et depuis un téléphone le lien s'ouvre toujours sur l'appareil. Contribution de oh-codes.
- **Plafond du compactage** : un réglage « compacter au plus tard à » (16K à 128K) sous l'interrupteur du compactage. Avec une très grande fenêtre de contexte, le seuil de 75 % n'était presque jamais atteint et les longues conversations devenaient lentes. Par défaut, rien ne change.
- **VRAM laissée à l'écran** (moteur MoE) : quand la carte principale gère aussi l'affichage, une réserve de 0,5 à 4 Go peut lui être laissée depuis les réglages du modèle installé. En mode carte d'aide, la réserve de l'installeur était jusqu'ici retirée sans possibilité d'en remettre une.

## Corrections

- **Interface redirigée vers une page externe** : une réponse ou une page lue contenant certaines balises HTML pouvait faire quitter l'interface dès son affichage (par exemple vers une page de connexion Microsoft). Seules les balises du markdown sont désormais interprétées, le reste s'affiche en texte.
- **Consolidation de Jean après un redémarrage** : la consolidation de nuit n'était programmée qu'après un échange avec Jean. Une machine redémarrée sans lui parler ne consolidait jamais. Elle est maintenant programmée dès le démarrage quand la mémoire a changé, avec les outils de Jean.
- **Jauge de contexte du moteur MoE** : en mode carte d'aide, la jauge affichait le contexte du preset (par exemple 168K) alors que le moteur s'arrête à 131K. La jauge et le compactage suivent maintenant la limite réelle.
- **Port 8090 occupé par un autre logiciel** : AJEAN ouvrait la page de ce logiciel à sa place et n'affichait pas son icône. Il vérifie maintenant que c'est bien AJEAN qui répond, et nomme sinon le programme qui tient le port. Le programme est aussi retrouvé sur un Windows non anglophone. Contribution de oh-codes.
- **Écritures longues en direct** : le corps d'une grosse écriture était relu en entier à chaque morceau reçu, ce qui ralentissait l'affichage en direct. Contribution de youtsuhodev.
- **Bulles d'aide** : une bulle « ? » ouverte d'un clic ne se refermait plus. Un second clic, un clic ailleurs ou Échap la referment.
- **Nouvelle app** : depuis le mode Rapide ou Modèle de base, où l'IA ne peut pas créer d'app, le bouton passe en mode Projet.
- **Moteur MoE en partage des couches** : la taille de lecture des prompts n'est plus forcée dans ce mode, où elle était défavorable.

## Interface

- Les informations de configuration du moteur MoE n'affichent plus la vision, déjà signalée par l'icône du preset.
- L'offre AJEAN LINK ne mentionne plus le support par chat, qui a pris fin.

## Mise à jour

```
ajean update
```

Non testé à ce jour : le choix du navigateur des liens sous Linux et macOS (testé par tests automatiques seulement), et la réserve de VRAM sur une machine dont la carte principale gère l'écran.
