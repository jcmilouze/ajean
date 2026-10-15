Nouveau moteur MoE pour Qwen3.8 Flash Next, plus rapide en génération comme en lecture des messages courts.

## Moteur AJEAN MoE 1.1

Le moteur optimisé de Qwen3.8 Flash Next passe en version 1.1. Mesures sur la machine de test (RTX 5060 Ti 16 Go + RTX 3070 8 Go, 46 Go de RAM, Swift 1.5 IQ3_XXS) :

- **Génération** : environ 70 à 76 jetons par seconde, contre 62 à 63 avec la 1.0.
- **Messages courts** : avec une deuxième carte, ses experts sont désormais calculés par elle pendant la lecture du prompt, au lieu d'être recopiés vers la carte principale par son lien PCIe. Un ajout de 100 jetons dans une conversation passe de 1,2 s à 0,85 s avant le premier mot, un ajout de 200 jetons de 2,0 s à 1,1 s.
- **Longs prompts** : environ 1 000 jetons par seconde à partir de 10 000 jetons, sans changement.
- **Experts gardés par la deuxième carte** : environ 230 de plus, en réduisant sa réserve de mémoire libre (l'encodeur d'images y tourne toujours).

La mise à jour est automatique : au premier lancement d'un modèle installé avec la 1.0, le moteur 1.1 est téléchargé (environ 340 Mo) et la configuration reprise. Les modèles déjà téléchargés servent tels quels. En cas d'échec, la 1.0 continue de tourner.

À savoir :

- Le moteur saute désormais, pendant la génération, certains experts de faible poids absents de la carte. Les réponses peuvent donc différer très légèrement de celles de la 1.0, pour un gain de vitesse de 10 à 20 %.
- En mode « experts de la deuxième carte hors RAM », le contexte est plafonné à 131 072 jetons : cette version demande environ 1 Go de RAM de plus, et au-delà la mémoire ne suffisait plus sur une machine de 46 Go. Le calcul de la RAM nécessaire avant installation a été ajusté en conséquence.
- Le raccourci du niveau de réflexion est toujours proposé avec ce moteur, et le niveau choisi est gardé dans le preset au lieu de revenir au défaut à chaque changement de modèle.

## Téléchargements

- **Pause et reprise** : un téléchargement de modèle peut être mis en pause puis repris là où il s'était arrêté. Aucune connexion ne reste ouverte pendant la pause.

## Jean

- **Consolidation la nuit seulement** : le rangement de la mémoire de Jean ne démarre plus qu'entre 3 h et 5 h (heure de l'utilisateur). En journée, il occupait les cartes et faisait croire à une utilisation inexpliquée.

## Cloud (Modal)

- **Modèle de brouillon et budget de sortie** : un preset Modal peut utiliser un modèle de brouillon GGUF pour le décodage spéculatif, et fixer un budget de jetons en sortie. Les presets existants ne sont pas redéployés.
- **URL de déploiement coupée** : une URL renvoyée sur deux lignes par Modal n'empêche plus la connexion.

## Mac

- **Panneau Appareil sur Apple Silicon** : la mémoire unifiée est affichée comme mémoire GPU, et une température inconnue n'est plus montrée.

## Corrections

- **Compactage** : les archives de rappel sont écrites en une seule fois, et le compactage se fait sans rappel au lieu d'échouer si la base est indisponible.

## Non testé

- L'installation à neuf du moteur MoE 1.1 n'a pas été testée de bout en bout : seule la mise à jour depuis la 1.0 l'a été.

## Mise à jour

    ajean update
