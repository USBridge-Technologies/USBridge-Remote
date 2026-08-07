# USBridge Remote (Beta)

![USBridge Remote](https://raw.githubusercontent.com/USBridge-Technologies/USBridge-Remote/main/assets/banner.png)

<div align="center">

[English](README.md) | [Deutsch](docs/README_DE.md) | [Français](docs/README_FR.md) | [Italiano](docs/README_IT.md) | [Español](docs/README_ES.md) | [Português (Brasil)](docs/README_PT_BR.md) | [Українська](docs/README_UA.md) | [Polski](docs/README_PL.md) | [日本語](docs/README_JA.md) | [한국어](docs/README_KO.md) | [简体中文](docs/README_ZH.md)

[![Beta](https://img.shields.io/badge/status-beta-orange)](https://github.com/USBridge-Technologies/USBridge-Remote/releases)
[![Patreon](https://img.shields.io/badge/Patreon-Support_Us-F96854?logo=patreon&logoColor=white)](https://www.patreon.com/USBridge_Technologies)
[![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)](#)
[![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)](#)
[![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)](#)
[![Android](https://img.shields.io/badge/Android-3DDC84?logo=android&logoColor=white)](#)
[![iOS](https://img.shields.io/badge/iOS-000000?logo=apple&logoColor=white)](#)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.com/invite/xqQ6ybkfWS)
<a href="https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?logo=crowdsupply&logoColor=white" alt="Crowd Supply"></a>

</div>

---

**USBridge Remote** est un client unifié haute performance pour gérer des machines distantes. Je l'ai conçu pour combiner l'**accès BIOS au niveau matériel** (via les dispositifs USBridge KVM) et le **bureau à distance basé sur logiciel** dans une interface unique et rationalisée.

 🖥️ **Besoin d'un contrôle BIOS au niveau matériel avant le démarrage de l'OS ?**  
 USBridge Remote s'intègre nativement avec **USBridge-KVM 2.0** pour une gestion hors bande, au niveau métal. 

[![Crowd Supply KVM 2.0](https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?style=for-the-badge&logo=crowdsupply&logoColor=white)](https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)


> ⚠️ **Logiciel Beta** — Il s'agit d'une version précoce. Attendez-vous à des bugs. Veuillez signaler les problèmes via [GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues) ou rejoignez notre [Discord](https://discord.com/invite/xqQ6ybkfWS) pour obtenir de l'aide.
> 
> ℹ️ **Note concernant les faux positifs de Windows Defender / Antivirus :**  
> Windows Defender peut signaler à tort `libva.dll` comme une menace (`Trojan:Win32/Wacatac.B!ml`) en raison de la détection heuristique/apprentissage automatique sur des binaires non signés. **C'est un faux positif.**  
> Nous avons soumis le fichier à Microsoft Security Intelligence pour un examen officiel et une mise sur liste blanche. En attendant, si votre antivirus supprime `libva.dll`, veuillez le restaurer depuis la quarantaine ou ajouter le dossier USBridge à votre liste d'exclusion antivirus.

---

## Télécharger

### Client
Le Client est l'interface de contrôle — installé sur votre station de travail ou votre ordinateur portable. Il gère les connexions, le bureau à distance en direct, le passage de périphériques virtuels et l'enregistrement des instantanés.

| Architecture | Windows | macOS | Linux | Android | iOS |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — |
| **ARM64** | — | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-selfupdate.apk) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |

## Agent

L'Agent s'exécute sur la machine cible — le serveur ou le PC que vous souhaitez accéder à distance. Il gère la capture d'écran, l'injection d'entrée et le réseau Tailscale.

| Architecture | Windows | macOS | Linux |
| :--- | :---: | :---: | :---: |
| **x86_64** | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip) | — | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage) |
| **ARM64** | — | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg) | — |

---

## Démo

<div align="center">
  <a href="https://youtu.be/1pV9PJeBr7M">
    <img src="https://img.youtube.com/vi/1pV9PJeBr7M/maxresdefault.jpg" alt="Démo USBridge Remote" style="max-width: 100%; border-radius: 8px;">
  </a>
</div>

---

## Dans les Médias

| Média | Mise en avant | Lien |
| :--- | :--- | :---: |
| **IlSoftware.it** (Italie) | *"USBridge Remote défie RustDesk et AnyDesk..."* — Revue indépendante approfondie louant la synergie entre l'agent logiciel & le matériel KVM, le support natif de Wayland et l'architecture P2P. | [Lire l'article](https://www.ilsoftware.it/alternativa-rustdesk-anydesk-usbridge/) |

---

## Fonctionnalités

<img width="2000" height="1046" alt="USBridge_ap4p" src="https://github.com/user-attachments/assets/2b4bfdf8-412f-4cd7-b4c4-3794d72475cc" />

**Un endroit pour tout** — J'ai unifié le flux de travail. Gérez le matériel USBridge KVM et les agents logiciels depuis un seul tableau de bord. Ajoutez une machine, connectez-vous, et c'est parti.

**Pas de limites, pas d'abonnements** — Entièrement gratuit. Pas de limites de temps de session, pas de plafonds de connexion, et aucun compte requis sur la machine cible.

**Vidéo à faible latence & Intégration Moonlight** — Profitez d'une résolution allant jusqu'à 2K avec un taux de 240 FPS ultra fluide et zéro latence perceptible. Mon moteur de streaming adaptatif tire parti de l'intégration native de Moonlight pour offrir des performances de bureau à distance inégalées et ultra-basse latence.

**Intégration Tailscale** — Tunnel P2P chiffré intégré. Connectez-vous à n'importe quelle machine dans le monde sans vous soucier du transfert de port ou des règles de pare-feu. Cela fonctionne automatiquement sur le LAN et via Internet.

**Presse-papiers partagé** — Copiez et collez sans effort entre votre machine locale et les cibles distantes. Il prend en charge entièrement le texte, les images et le transfert de fichiers dès la sortie de la boîte.

**Support multi-écrans** — J'ai ajouté la possibilité de basculer entre plusieurs affichages. Si la machine cible a plusieurs moniteurs, vous pouvez maintenant facilement sélectionner lequel visualiser directement depuis les paramètres de connexion. 

<img width="2080" height="1170" alt="Capture d'écran 2026-05-03 20112н0" src="https://github.com/user-attachments/assets/06dc3de0-2be9-42f7-a897-830a0a6f2bc7" />


---

## Support Wayland (Sans invites)

La plupart des agents de bureau à distance sur Linux ont du mal avec Wayland ou vous bombardent constamment d'invites de permission et de fenêtres de confirmation chaque fois qu'une session commence. 

J'ai conçu l'Agent USBridge pour prendre en charge Wayland nativement. Il gère la capture d'écran complète et l'injection d'entrée dès la sortie de la boîte **sans aucune invite de permission ennuyeuse** ou confirmations manuelles. Ça fonctionne tout simplement.

---

## Démarrage rapide

1. **Installez l'Agent** sur la machine que vous souhaitez accéder à distance. Lancez-le — il affichera un jeton de connexion et une adresse Tailscale. Connectez Tailscale si vous avez besoin d'accès via Internet.

2. **Installez le Client** sur votre station de travail, ordinateur portable ou téléphone.

3. **Ajoutez une connexion** — entrez l'adresse IP ou l'adresse Tailscale affichée dans la fenêtre de l'Agent. C'est tout.

---

##  Feuille de route du projet

Je gère les plans de développement logiciel et les fonctionnalités à venir dans un tableau de bord ouvert. Si vous souhaitez voir ce qui est actuellement en développement, ce qui est prévu, ou suivre l'état des fonctionnalités à venir, consultez la feuille de route en direct :

 **[Voir la feuille de route USBridge Remote](https://github.com/orgs/USBridge-Technologies/projects/3)**

---

## Communauté & Tests Beta

Rejoignez notre Discord pour obtenir le rôle de **Testeur Beta**, signaler des bugs et m'aider à façonner la feuille de route :

**[discord.com/invite/xqQ6ybkfWS](https://discord.com/invite/xqQ6ybkfWS)**

---

## Liens

- 🌐 [Site Officiel](https://usbridge.io)
- ❤️ [Page Patreon](https://www.patreon.com/USBridge_Technologies)
- 🛒 [USBridge KVM 2.0 sur Crowd Supply](https://crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)
- 💬 [Discord](https://discord.com/invite/xqQ6ybkfWS)