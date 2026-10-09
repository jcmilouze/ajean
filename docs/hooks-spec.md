# Spec : hooks d'outils (palier 1)

Statut : brouillon, fork local uniquement. Base : v0.18.0-5 (`e77aaec`).

## But
Laisser l'utilisateur brancher des commandes externes sur la boucle d'outils de l'agent pour bloquer ou observer un appel (`bash`, `write`, `edit`, outils MCP…). Aucun runtime embarqué : un hook est un processus.

Hors périmètre : panneaux d'UI, commandes slash, installation de plugins, modification des arguments, confirmation interactive (l'UI n'a pas de dialogue d'approbation). Ces points relèvent des paliers 2 et 3.

## Points d'insertion (vérifiés)
Boucle d'exécution : `internal/ajean/llm_client.go`, `for _, tc := range tcs`.

| Événement | Où | Effet |
|---|---|---|
| `PreToolUse` | après le contrôle de déduplication (`doneCalls`, ~l.1588), avant `switch tc.Function.Name` (~l.1594) | peut refuser l'appel |
| `PostToolUse` | juste après `result = capToolResult(result)` (~l.1761) | observation seule, envoyée dans la file asynchrone |

Un refus reprend le schéma des garde-fous existants (`badArgs`, dédup) : résultat `[erreur] …`, événement `ToolUsed{Done:true}`, `toolMsg` ajouté à `messages` et `extra`, puis `continue`. Le préfixe `[erreur]` garde l'appel hors de `doneCalls` : le modèle peut reformuler et réessayer.

Les appels refusés par `isJeanReflecting` ou `badArgs` n'atteignent pas le hook (ils sont rejetés avant).

## File d'événements `PostToolUse`
`PostToolUse` passe par une file asynchrone, pour qu'un hook lent (audit, notification) ne retarde jamais l'étape suivante du modèle. `PreToolUse` reste synchrone.

- **Un seul worker** (une goroutine) : l'ordre des événements est préservé, y compris entre conversations.
- **Canal borné** (256 par défaut). Quand il est plein, on abandonne l'événement le plus récent, on incrémente un compteur `dropped` et on écrit une ligne dans les logs. L'agent n'est jamais bloqué par la file.
- **Instantané** : l'événement copie `tool`, `args`, `label` et `result` (des chaînes) avant l'envoi. Rien n'est partagé avec la boucle d'outils.
- **Contexte propre** : le worker n'utilise pas le contexte du tour, qui est annulé par le bouton stop ou en fin de tour. Chaque hook garde son `timeoutSec`.
- **Arrêt** : AJEAN n'a pas de chemin d'arrêt propre (`mcpCloseAll` n'est appelé nulle part). Il n'y a donc pas de vidage à la fermeture : `flushPostHooks(timeout)` existe pour les tests et pour un futur arrêt propre.
- **Pas de durabilité** : un arrêt ou un crash du processus perd les événements en attente. Si un journal fiable est nécessaire, le hook écrit lui-même sur disque. Un journal intégré (JSONL) serait un autre palier.
- **Option** `"async": false` sur un hook `PostToolUse` pour le forcer en synchrone (utile aux tests).
- **Observabilité** : `ajean hooks status` affiche la profondeur de la file, les événements traités, abandonnés et en erreur.

Fichier : `hooks_queue.go` (~60 lignes), séparé de `hooks.go` pour rester sous la limite de 200 lignes par fichier.

## Configuration
Fichier : `$AJEAN_HOME/hooks/hooks.json`. Absent = aucun hook, aucun coût.

```json
{
  "PreToolUse": [
    { "matcher": "^bash$", "command": "powershell -File C:\\hooks\\blast-radius.ps1", "timeoutSec": 10 },
    { "matcher": "^(write|edit)$", "command": "node C:\\hooks\\protect-paths.js", "onError": "deny" }
  ],
  "PostToolUse": [
    { "matcher": ".*", "command": "C:\\hooks\\audit.exe" }
  ]
}
```

- `matcher` : regex Go sur le nom exposé de l'outil. Les outils MCP utilisent le nom de `mcpExposedName`.
- `timeoutSec` : 10 par défaut, 60 au maximum.
- `onError` (`allow` par défaut, ou `deny`) : comportement si le hook plante, dépasse le délai ou renvoie un JSON illisible. À mettre à `deny` pour un garde-fou de sécurité.
- Les hooks d'un même événement s'exécutent dans l'ordre. Le premier refus arrête la chaîne.

## Protocole
Le hook est lancé avec `newShellCmd` (même shell que `bash` : `cmd.exe` sous Windows). JSON sur stdin :

```json
{ "event": "PreToolUse", "tool": "bash", "args": { "command": "rm -rf build" },
  "label": "rm -rf build", "cwd": "<workspace>", "jean": false }
```

`PostToolUse` ajoute `"result"` (déjà tronqué par `capToolResult`) et `"error": true|false` (préfixe `[erreur]`).

Sortie du hook :
- code 0 et stdout vide : `allow`.
- code 0 et stdout `{"decision":"deny","reason":"…"}` : refus.
- code 2 : refus, la raison est lue sur stderr.
- autre code : erreur du hook, traitée selon `onError`.

Le modèle reçoit `[erreur] bloqué par un hook : <reason>`. La raison est tronquée à 500 caractères.

## Contraintes d'implémentation
- Reprendre le `WaitDelay = 2s` et le contexte du tour de `runShell` : un hook qui lance un processus en arrière-plan ne doit jamais bloquer le tour, et le bouton stop doit l'interrompre.
- Taille de stdin bornée, stdout borné à 64 Ko.
- Chargement avec cache sur `mtime`, relu entre deux appels sans redémarrage.
- Pas de hook pendant `isJeanReflecting` (révision mémoire), comme pour les autres outils.

## Sécurité
Un hook s'exécute avec les droits de l'utilisateur, donc :
1. **Protéger le dossier `hooks/`** comme `memory/` et `scripts/` : étendre `guardToolOnlyPath` / `guardToolOnlyCommand` (`chat_tools.go`) pour que `bash`, `write` et `edit` ne puissent pas le toucher. Sinon le modèle pourrait écrire ou supprimer ses propres garde-fous.
2. **Aucune écriture de hooks depuis l'API web ni depuis ajean.link.** Édition par fichier local ou par la CLI seulement.
3. Variable `AJEAN_NO_HOOKS=1` : désactive tout (équivalent d'un safe-mode).
4. Un hook de refus ne remplace pas un vrai sandbox : le modèle peut contourner un matcher sur `bash` en passant par un autre outil. Les exemples doivent le dire.

## Fichiers
| Fichier | Changement |
|---|---|
| `hooks_config.go` (nouveau) | `hooks.json` : types, chargement avec cache `mtime`, `matchingHooks` |
| `hooks.go` (nouveau) | `runPreHooks(ctx, ev) string` (vide = autorisé), `runHook`, protocole de décision |
| `hooks_queue.go` (nouveau) | file bornée, worker, `drain`, `runPostHooks` |
| `hooks_test.go` (nouveau) | voir plan de test |
| `llm_client.go` | deux appels, ~10 lignes. Le fichier fait déjà 1897 lignes et reste le point de conflit avec l'amont. |
| `chat_tools.go` | protection du dossier `hooks/` |
| `cli_app.go` | `ajean hooks [list\|test <outil> <json>]` |

## Plan de test
- `go test ./internal/ajean -run Hook`, avec des hooks de test sur le shell :
  - refus par code 2 et par JSON ;
  - autorisation ;
  - délai dépassé avec `onError` à `allow`, puis à `deny` ;
  - JSON invalide ;
  - matcher qui ne correspond pas ;
  - processus d'arrière-plan qui ne bloque pas ;
  - file : ordre préservé, abandon quand elle est pleine sans bloquer l'appelant, vidage à l'arrêt, hook lent qui ne retarde pas la boucle (`go test -race`).
- Un test vérifie que `write` vers `hooks/hooks.json` est refusé.
- E2E manuel : un hook qui bloque `rm -rf`, l'agent reçoit le refus et ne passe pas à l'exécution.
- Comparer `ajean-fork.exe version` avec `Version` dans `run.go` avant tout test (voir la note sur le binaire en retard).

## Questions ouvertes
- Le terminal (`cli_chat.go`), les tâches planifiées (`tasks_run.go`) et les révisions de Jean (`jean_reflect.go`) appellent tous `runChat` : les hooks s'y appliquent. Les révisions de Jean n'atteignent pas les hooks pour les outils refusés par `isJeanReflecting`.
- `ajean hooks [list | test <outil> ['<json>']]` : implémenté (`hooks_cli.go`). `list` signale les événements inconnus (une faute de frappe désactive un hook en silence) ; `test` rejoue les hooks `PreToolUse` sur un appel simulé sans exécuter l'outil. Pas de sous-commande `status` : les compteurs de la file vivent dans le processus de l'interface, une CLI séparée afficherait toujours zéro. Les exposer demanderait un endpoint dans l'API web.
- `UserPromptSubmit` : point d'entrée non localisé. Reporté tant qu'il n'est pas identifié.
- Hooks par projet (`projects/`) : utile, mais pose la question de la confiance dans un dossier de projet. Reporté.

## Exécution
Mode 3 (touche à l'exécution de l'agent). Tag `backup-pre-hooks`, branche `feature/hooks`, une tranche verticale à la fois : `PreToolUse` seul avec protection du dossier d'abord, puis `PostToolUse`, puis la CLI.
