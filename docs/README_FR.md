# USBridge Remote

<div align="center">
  <img src="./assets/banner6.svg" width="1400" alt="USBridge Remote: bureau à distance à faible latence et KVM-over-IP">
</div>

<div align="center">

[English](README.md) | [Deutsch](docs/README_DE.md) | [Français](docs/README_FR.md) | [Italiano](docs/README_IT.md) | [Español](docs/README_ES.md) | [Português (Brasil)](docs/README_PT_BR.md) | [Українська](docs/README_UA.md) | [Polski](docs/README_PL.md) | [日本語](docs/README_JA.md) | [한국어](docs/README_KO.md) | [简体中文](docs/README_ZH.md)

[![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)](#)
[![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)](#)
[![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)](#)
[![Android](https://img.shields.io/badge/Android-3DDC84?logo=android&logoColor=white)](https://play.google.com/store/apps/details?id=io.usbridge.client)
[![iOS](https://img.shields.io/badge/iOS-000000?logo=apple&logoColor=white)](https://apps.apple.com/us/app/usbridge-remote-desktop/id6787665935)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.com/invite/xqQ6ybkfWS)

</div>

---

**USBridge Remote** est un écosystème de **bureau à distance** et de **KVM-over-IP** à faible latence : un client pour Windows, macOS, Linux, Android, iOS et le navigateur, un agent hôte avec un streamer de jeu **Sunshine / Moonlight** intégré, et un firmware IP-KVM pour les cartes **Radxa Zero 3W**, **Radxa Cubie A7Z** et **Sipeed NanoKVM**. Il combine un **accès BIOS au niveau matériel** et un **bureau à distance logiciel** dans une seule interface, avec un passthrough USB pour les manettes de jeu, les tablettes Wacom, les MIDI et les microphones, et un réseau P2P Tailscale sans redirection de port.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="Fonctionnalités de USBridge Remote : bureau à distance, streaming de jeux, passthrough USB, KVM matériel">
</div>


## Télécharger

### Client
Le Client est l'interface de contrôle — installé sur votre station de travail ou votre ordinateur portable (ou exécuté directement dans votre navigateur). Il gère les connexions, le bureau à distance en direct, le passthrough de dispositifs virtuels et le registre des instantanés.

| Architecture | Windows | macOS | Linux | Android | iOS | Navigateur Web |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Ouvrir l'application](https://web.usbridge.io) |
| **ARM64** | — | [Télécharger](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Ouvrir l'application](https://web.usbridge.io) |

Vous préférez un APK direct sans compte Play Store ? Une version auto-mise à jour est également publiée sur la [dernière version](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Client Web sans installation** : Aucune installation requise. Ouvrez simplement [web.usbridge.io](https://web.usbridge.io) pour vous connecter instantanément. *(Remarque : Le client web fonctionne avec certaines limitations de fonctionnalités et de performances en raison de la sécurité du navigateur et des contraintes WebRTC. Pour une expérience complète et sans compromis, utilisez les applications natives).* Le client web atteint un Agent via son nom de confiance `<label>.device.usbridge.io` : l'Agent obtient ce certificat la première fois qu'un navigateur y accède, et seulement pendant que vous êtes connecté à votre compte USBridge dans l'Agent — donc la toute première tentative peut prendre quelques minutes. Consultez la ligne **Statut → Certificat** de l'Agent, ou la [documentation de l'Agent](agent/docs/README.md#platform-notes-from-the-top-level-readme) pour plus de détails. Un KVM USBridge sert également le client web lui-même à `https://<kvm>:9443/` et passe à son propre nom de confiance lorsqu'il le peut.

## Agent

L'Agent s'exécute sur la machine cible — le serveur ou le PC que vous souhaitez accéder à distance. Il gère la capture d'écran, l'injection d'entrée et le réseau Tailscale.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Architecture</th>
          <th>Windows</th>
          <th>macOS</th>
          <th>Linux</th>
        </tr>
        <tr>
          <td><b>x86_64</b></td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Télécharger</a></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">Télécharger</a></td>
        </tr>
        <tr>
          <td><b>ARM64</b></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Télécharger</a></td>
          <td>—</td>
        </tr>
      </table>
    </td>
    <!-- Right Column: Image -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="Interface de l'Agent USBridge" width="100%">
    </td>
  </tr>
</table>

## Fonctionnalités

**Encodage accéléré par matériel :** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · plus le logiciel **H.264 (OpenH264)** pour les VM et les machines sans encodeur GPU

<table>
  <tr>
    <td width="33%" valign="top">
      <code>Fonctionnalité de base</code><br><br>
      <b>SUNSHINE OPEN-SOURCE</b><br>
      <i>Streaming de jeux standard</i><br><br>
      ✓ Streamer Sunshine open-source intégré<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">Streamers alternatifs</a> : Vibepollo (fork Sunshine avec Apollo intégré, Windows) et punktfunk (compatible Moonlight, Windows & Linux), changement en un clic<br>
      ✓ Benchmark intégré pour comparer Sunshine, RustShine, Vibepollo et punktfunk sur votre propre matériel et réseau<br>
      ✓ Accès bureau & jeux à faible latence<br>
      ✓ Presse-papiers de fichiers & texte bidirectionnels<br>    
      ✓ Changement d'affichage multi-moniteurs<br>
      ✓ Réseau P2P Tailscale intégré<br>
      ✓ Accès invité : invitez des personnes par e-mail avec un rôle et une date de fin<br>
      ✓ Support Wayland natif (capture sans invite)
    </td>
    <td width="33%" valign="top">
      <code>+ FONCTIONNALITÉS DE BASE</code><br><br>
      <b>USBRIDGE GRATUIT</b><br>
      <i>Moteur de streaming basé sur Rust</i><br><br>
      ✓ Protocole distant personnalisé de connexion instantanée<br>
      ✓ Passthrough de périphériques USB <a href="https://www.usbridge.io/usb-passthrough/">bruts</a><br>
      ✓ Streaming adaptatif optimisé pour la stabilité Wi-Fi<br>
      ✓ HDR10<br>
      ✓ Précision des couleurs chroma 4:4:4 sans perte<br>
      ✓ Passthrough de microphone (client → hôte)<br>
      ✓ Passthrough de dispositif MIDI (client → hôte)<br>
      ✓ Encodage GPU : NVENC, AMF, QSV, VAAPI<br>
      ✓ H.264 logiciel (OpenH264) pour les VM sans GPU<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> codec en onde à faible latence (par Valve)<br>
      ✓ Gestion d'affichage virtuel sans tête<br>
      ✓ Passthrough de contrôleur de jeu à faible latence<br>
      ✓ Accès client navigateur web (aucune installation requise)<br>
      ✓ Accès pré-login Windows (entrée de credentials sécurisée)
    </td>
    <td width="33%" valign="top">
      <code>+ FONCTIONNALITÉS DE BASE</code> <code>+ FONCTIONNALITÉS GRATUITES</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Flux de travail professionnels</i><br><br>
      ✓ Support de tablette Wacom avec pression & inclinaison (<a href="https://www.usbridge.io/macos-wacom-support/">macOS aussi</a>)<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">Firmware KVM matériel</a> pour Radxa Zero 3W, Radxa Cubie A7Z et Sipeed NanoKVM : accès BIOS/UEFI, médias virtuels ISO, instantanés Btrfs (19 $ à vie par appareil, évaluation gratuite de 24 h)<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">Streamer Moonlight matériel</a> sur ces cartes : toute source HDMI (PC, PS5, Xbox, Switch) vers Moonlight à 1080p 60 FPS, sans logiciel sur la source<br><br>
    </td>
  </tr>
</table>

## Démarrage rapide

1. **Installez l'Agent** sur la machine que vous souhaitez accéder à distance. Lancez-le — il affichera un jeton de connexion et une adresse Tailscale. Connectez Tailscale si vous avez besoin d'accès via Internet.

2. **Installez le Client** sur votre station de travail, ordinateur portable ou téléphone.

3. **Ajoutez une connexion** — entrez l'adresse IP ou l'adresse Tailscale affichée dans la fenêtre de l'Agent. C'est tout.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="Démarrage rapide de USBridge Remote : installer l'agent, installer le client, se connecter">
</div>

## Intégration matérielle

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Contrôle BIOS au niveau matériel</b><br>
      USBridge Remote s'intègre nativement avec l'appareil USBridge-KVM 2.0 pour une gestion hors bande, bare-metal avant le démarrage de l'OS. C'est également le premier streamer Moonlight matériel : le client Moonlight standard se couple avec le KVM et diffuse toute source HDMI (un PC ou une console de jeu) sans logiciel dessus.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Acheter USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>Firmware IP-KVM DIY</b><br>
      Flashez le firmware officiel sur une carte compatible (Radxa Zero 3W, Radxa Cubie A7Z) avec un dongle de capture HDMI vers USB pour construire votre propre IP-KVM. Sur un <b>Sipeed NanoKVM</b>, il remplace le firmware d'origine : vidéo fluide à 60 FPS au lieu d'une image saccadée, audio et microphone, manettes de jeu, passthrough USB et support Moonlight (<a href="https://www.usbridge.io/hardware-agent/">téléchargez le firmware</a>).<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Obtenir le Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>Dongle matériel USB/IP (ESP32-S3)</b><br>
      Le passthrough USB logiciel atteint une limite sur macOS : il n'y a pas de pilote VHCI auquel se rattacher, donc une tablette Wacom distante (ou tout autre dispositif USB) ne peut pas être présentée comme un véritable matériel. Ce dongle se branche sur le Mac et fait en matériel ce que <code>vhci-hcd</code> fait en logiciel ailleurs — le dispositif exporté s'énumère avec son propre VID/PID, lié par le propre pilote de macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="firmware & docs esp32-acm"></a>
    </td>
  </tr>
</table>

### NanoKVM avec firmware USBridge : 60 FPS au lieu de latence

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Vidéo : Sipeed NanoKVM flashé avec le firmware USBridge diffuse à 60 FPS avec presque aucune latence d'entrée"></a>
</div>

Une vidéo de 2 minutes : un Sipeed NanoKVM avec une forte latence dans un laboratoire à domicile devient un KVM à 60 FPS sur IP avec presque aucune latence d'entrée après avoir flashé le firmware USBridge, suffisamment rapide pour jouer avec une manette de jeu. Le firmware pour le NanoKVM, Radxa Zero 3W et Radxa Cubie A7Z est sur la [page de l'Agent Matériel](https://www.usbridge.io/hardware-agent/).

## FAQ

<details>
<summary><b>USBridge fonctionne-t-il avec Moonlight et Sunshine ?</b></summary>

Oui. L'Agent expédie le streamer Sunshine open-source, donc le client Moonlight standard s'y connecte. Vibepollo (un fork Sunshine avec Apollo intégré) et punktfunk sont également intégrés et changent en un clic.
</details>

<details>
<summary><b>Y a-t-il un streamer Moonlight matériel ?</b></summary>

Oui. Un USBridge KVM 2.0, ou un Radxa Zero 3W, Radxa Cubie A7Z ou Sipeed NanoKVM flashé avec le firmware USBridge, est un hôte GameStream compatible Sunshine pour tout ce qui est sur son entrée HDMI : un PC, PS5, Xbox ou Switch. Il diffuse en 1080p 60 FPS avec audio vers le client Moonlight standard, sans logiciel sur la source. Voir [Streamer Moonlight Matériel](https://www.usbridge.io/moonlight-hardware-streamer/).
</details>

<details>
<summary><b>Quel streamer est le plus rapide sur mon PC ?</b></summary>

Cela dépend du GPU, du pilote et du réseau. Le benchmark intégré exécute Sunshine, RustShine, Vibepollo et punktfunk sur votre propre matériel et réseau et les compare. Voir [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/).
</details>

<details>
<summary><b>Quels GPU sont pris en charge pour l'encodage ?</b></summary>

NVIDIA NVENC, AMD AMF, Intel QSV et VAAPI sur Linux. Les machines virtuelles et les PC sans encodeur GPU utilisent H.264 logiciel (OpenH264).
</details>

<details>
<summary><b>Qu'est-ce qui est gratuit et qu'est-ce qui nécessite Pro ?</b></summary>

La couleur 4:4:4, HDR10, PyroWave, les manettes de jeu, le microphone, le MIDI et le passthrough USB brut sont gratuits. Les tablettes Wacom avec pression et inclinaison, et le firmware KVM matériel (19 $ à vie par appareil, évaluation gratuite de 24 h), sont Pro.
</details>

<details>
<summary><b>Puis-je transformer un Sipeed NanoKVM ou une carte Radxa en IP-KVM ?</b></summary>

Oui. Flashez le firmware USBridge depuis la [page de l'Agent Matériel](https://www.usbridge.io/hardware-agent/). Vous obtenez un accès BIOS/UEFI, des médias virtuels ISO et IMG, des instantanés Btrfs immuables, une vidéo à 60 FPS, de l'audio et un passthrough USB. Les cartes Radxa nécessitent un dongle de capture HDMI vers USB.
</details>

<details>
<summary><b>Ai-je besoin de redirection de port ?</b></summary>

Non. Les connexions passent par Tailscale peer-to-peer, avec traversée NAT automatique.
</details>

<details>
<summary><b>Puis-je partager l'accès à un ordinateur avec quelqu'un d'autre ?</b></summary>

Oui. Invitez des personnes par e-mail avec un rôle et une date de fin ; les invités rejoignent via USBridge et n'ont pas besoin de compte Tailscale.
</details>

## Ressources & Licence

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Développement & Feuille de route</b><br>
      Je maintiens un tableau de bord public pour suivre toutes les fonctionnalités prévues, les mises à jour architecturales et les calendriers de publication.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Voir la feuille de route en direct</a>
    </td>
    <td width="33%" valign="top">
      <b>Liens du projet</b><br>
      <ul>
        <li><a href="https://usbridge.io">Site officiel</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Tests bêta & Bugs)</a></li>
        <li><a href="https://www.reddit.com/r/USBridge/">Reddit</a></li>
        <li><a href="https://www.youtube.com/@KVMUSBridge">YouTube</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Soutien Patreon</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>Licence (GPLv3)</b><br>
      Ce projet est sous licence <b>GPLv3</b>. Le client multiplateforme intègre le code de <code>moonlight-common-c</code> (également GPLv3).<br><br>
      <a href="LICENSE">Voir le fichier de licence</a>
    </td>
  </tr>
</table>