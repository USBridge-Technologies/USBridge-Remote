# USBridge Remote

<div align="center">
  <img src="./assets/banner6.svg" width="1400" alt="USBridge Remote: desktop remoto a bassa latenza e KVM-over-IP">
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

**USBridge Remote** è un ecosistema di **desktop remoto** e **KVM-over-IP** a bassa latenza: un client per Windows, macOS, Linux, Android, iOS e il browser, un agente host con uno streamer di giochi **Sunshine / Moonlight** integrato e firmware IP-KVM per le schede **Radxa Zero 3W**, **Radxa Cubie A7Z** e **Sipeed NanoKVM**. Combina l'**accesso al BIOS a livello hardware** e il **desktop remoto software** in un'unica interfaccia, con passthrough USB per gamepad, tablet Wacom, MIDI e microfoni, e networking P2P Tailscale senza port forwarding.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="Funzionalità di USBridge Remote: desktop remoto, streaming di giochi, passthrough USB, KVM hardware">
</div>


## Download

### Client
Il Client è l'interfaccia di controllo — installata sulla tua workstation o laptop (o eseguita direttamente nel tuo browser). Gestisce le connessioni, il desktop remoto live, il passthrough di dispositivi virtuali e il registro degli snapshot.

| Architettura | Windows | macOS | Linux | Android | iOS | Web Browser |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Apri App](https://web.usbridge.io) |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Apri App](https://web.usbridge.io) |

Preferisci un APK diretto senza un account Play Store? Una build auto-aggiornante è anche pubblicata nella [versione più recente](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Client Web Zero-Install**: Nessuna installazione richiesta. Basta aprire [web.usbridge.io](https://web.usbridge.io) per connettersi istantaneamente. *(Nota: Il client web opera con alcune limitazioni di funzionalità e prestazioni a causa della sandbox di sicurezza del browser e delle restrizioni di WebRTC. Per un'esperienza completa e senza compromessi, utilizza le app native).* Il client web raggiunge un Agente attraverso il suo nome fidato `<label>.device.usbridge.io`: l'Agente ottiene quel certificato la prima volta che un browser si connette a esso, e solo mentre sei connesso al tuo account USBridge nell'Agente — quindi il primo tentativo può richiedere alcuni minuti. Consulta la riga **Stato → Certificato** dell'Agente, o la [documentazione dell'Agente](agent/docs/README.md#platform-notes-from-the-top-level-readme) per dettagli. Un KVM USBridge serve anche il client web stesso a `https://<kvm>:9443/` e passa al proprio nome fidato quando può.

## Agente

L'Agente viene eseguito sulla macchina target — il server o PC a cui desideri accedere in remoto. Gestisce la cattura dello schermo, l'iniezione degli input e il networking Tailscale.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Architettura</th>
          <th>Windows</th>
          <th>macOS</th>
          <th>Linux</th>
        </tr>
        <tr>
          <td><b>x86_64</b></td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Download</a></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">Download</a></td>
        </tr>
        <tr>
          <td><b>ARM64</b></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Download</a></td>
          <td>—</td>
        </tr>
      </table>
    </td>
    <!-- Right Column: Image -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="Interfaccia dell'Agente USBridge" width="100%">
    </td>
  </tr>
</table>

## Funzionalità

**Codifica accelerata hardware:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · più software **H.264 (OpenH264)** per VM e macchine senza un codificatore GPU

<table>
  <tr>
    <td width="33%" valign="top">
      <code>FUNZIONALITÀ DI BASE</code><br><br>
      <b>SUNSHINE OPEN-SOURCE</b><br>
      <i>Streaming di giochi standard</i><br><br>
      ✓ Streamer Sunshine open-source integrato<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">Streamer alternativi</a>: Vibepollo (fork di Sunshine con Apollo integrato, Windows) e punktfunk (compatibile con Moonlight, Windows e Linux), cambio con un clic<br>
      ✓ Benchmark integrato per confrontare Sunshine, RustShine, Vibepollo e punktfunk sul tuo hardware e rete<br>
      ✓ Accesso a desktop e giochi a bassa latenza<br>
      ✓ Appunti di file e testo bidirezionali<br>    
      ✓ Cambio di visualizzazione multi-monitor<br>
      ✓ Networking P2P Tailscale integrato<br>
      ✓ Accesso ospite: invita persone via e-mail con un ruolo e una data di fine<br>
      ✓ Supporto nativo per Wayland (cattura senza prompt)
    </td>
    <td width="33%" valign="top">
      <code>+ FUNZIONALITÀ DI BASE</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Motore di streaming basato su Rust</i><br><br>
      ✓ Protocollo remoto personalizzato per connessione istantanea<br>
      ✓ Passthrough di <a href="https://www.usbridge.io/usb-passthrough/">dispositivi periferici USB</a><br>
      ✓ Streaming adattivo ottimizzato per la stabilità del Wi-Fi<br>
      ✓ HDR10<br>
      ✓ Accuratezza del colore 4:4:4 senza perdita<br>
      ✓ Passthrough del microfono (client → host)<br>
      ✓ Passthrough del dispositivo MIDI (client → host)<br>
      ✓ Codifica GPU: NVENC, AMF, QSV, VAAPI<br>
      ✓ Software H.264 (OpenH264) per VM senza GPU<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> codec wavelet a bassa latenza (di Valve)<br>
      ✓ Gestione di display virtuali senza testa<br>
      ✓ Passthrough del controller gamepad a bassa latenza<br>
      ✓ Accesso client web browser (nessuna installazione richiesta)<br>
      ✓ Accesso pre-login a Windows (inserimento credenziali sicuro)
    </td>
    <td width="33%" valign="top">
      <code>+ FUNZIONALITÀ DI BASE</code> <code>+ FUNZIONALITÀ FREE</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Flussi di lavoro professionali</i><br><br>
      ✓ Supporto per tablet Wacom con pressione e inclinazione (<a href="https://www.usbridge.io/macos-wacom-support/">anche per macOS</a>)<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">Firmware KVM hardware</a> per Radxa Zero 3W, Radxa Cubie A7Z e Sipeed NanoKVM: accesso BIOS/UEFI, media virtuali ISO, snapshot Btrfs ($19 a vita per dispositivo, 24 ore di valutazione gratuita)<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">Streamer hardware Moonlight</a> su quelle schede: qualsiasi sorgente HDMI (PC, PS5, Xbox, Switch) a Moonlight a 1080p 60 FPS, senza software sulla sorgente<br><br>
    </td>
  </tr>
</table>

## Avvio Veloce

1. **Installa l'Agente** sulla macchina a cui desideri accedere in remoto. Avvialo — mostrerà un token di connessione e un indirizzo Tailscale. Collega Tailscale se hai bisogno di accesso su Internet.

2. **Installa il Client** sulla tua workstation, laptop o telefono.

3. **Aggiungi una connessione** — inserisci l'indirizzo IP o Tailscale mostrato nella finestra dell'Agente. Questo è tutto.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="Avvio veloce di USBridge Remote: installa l'agente, installa il client, connettiti">
</div>

## Integrazione Hardware

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Controllo BIOS a livello hardware</b><br>
      USBridge Remote si integra nativamente con l'appliance USBridge-KVM 2.0 per la gestione out-of-band, bare-metal prima dell'avvio del sistema operativo. È anche il primo streamer hardware Moonlight: il client Moonlight stock si accoppia con il KVM e trasmette qualsiasi sorgente HDMI (un PC o una console di gioco) senza software su di essa.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Acquista USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>Firmware IP-KVM fai da te</b><br>
      Flash il firmware ufficiale su una scheda compatibile (Radxa Zero 3W, Radxa Cubie A7Z) con un dongle di cattura HDMI-to-USB per costruire il tuo IP-KVM. Su un <b>Sipeed NanoKVM</b> sostituisce il firmware stock: video fluido a 60 FPS invece di un'immagine a scatti, audio e microfono, gamepad, passthrough USB e supporto Moonlight (<a href="https://www.usbridge.io/hardware-agent/">scarica il firmware</a>).<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Ottieni il Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>Dongle hardware USB/IP (ESP32-S3)</b><br>
      Il passthrough USB software incontra un limite su macOS: non c'è driver VHCI a cui collegarsi, quindi un tablet Wacom remoto (o altro dispositivo USB) non può essere presentato come hardware reale. Questo dongle si collega al Mac e fa in hardware ciò che <code>vhci-hcd</code> fa in software altrove — il dispositivo esportato si enumera con il proprio VID/PID, legato dal driver stesso di macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="firmware & documentazione esp32-acm"></a>
    </td>
  </tr>
</table>

### NanoKVM con firmware USBridge: 60 FPS invece di lag

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Video: Sipeed NanoKVM flashato con firmware USBridge trasmette a 60 FPS con quasi nessun lag di input"></a>
</div>

Un video di 2 minuti: un Sipeed NanoKVM con forte lag in un laboratorio domestico diventa un KVM over IP a 60 FPS con quasi nessun lag di input dopo aver flashato il firmware USBridge, abbastanza veloce da giocare con un gamepad. Il firmware per il NanoKVM, Radxa Zero 3W e Radxa Cubie A7Z è sulla [pagina Hardware Agent](https://www.usbridge.io/hardware-agent/).

## FAQ

<details>
<summary><b>Funziona USBridge con Moonlight e Sunshine?</b></summary>

Sì. L'Agente include lo streamer Sunshine open-source, quindi il client Moonlight stock si connette ad esso. Anche Vibepollo (un fork di Sunshine con Apollo integrato) e punktfunk sono integrati e cambiano con un clic.
</details>

<details>
<summary><b>Esiste uno streamer hardware Moonlight?</b></summary>

Sì. Un USBridge KVM 2.0, o un Radxa Zero 3W, Radxa Cubie A7Z o Sipeed NanoKVM flashato con firmware USBridge, è un host GameStream compatibile con Sunshine per qualsiasi sorgente sul suo input HDMI: un PC, PS5, Xbox o Switch. Trasmette a 1080p 60 FPS con audio al client Moonlight stock, senza software sulla sorgente. Vedi [Streamer hardware Moonlight](https://www.usbridge.io/moonlight-hardware-streamer/).
</details>

<details>
<summary><b>Quale streamer è il più veloce sul mio PC?</b></summary>

Dipende dalla GPU, dal driver e dalla rete. Il benchmark integrato esegue Sunshine, RustShine, Vibepollo e punktfunk sul tuo hardware e rete e li confronta. Vedi [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/).
</details>

<details>
<summary><b>Quali GPU sono supportate per la codifica?</b></summary>

NVIDIA NVENC, AMD AMF, Intel QSV e VAAPI su Linux. Le macchine virtuali e i PC senza un codificatore GPU utilizzano software H.264 (OpenH264).
</details>

<details>
<summary><b>Cosa è gratuito e cosa richiede Pro?</b></summary>

Il colore 4:4:4, HDR10, PyroWave, gamepad, microfono, MIDI e passthrough USB raw sono gratuiti. I tablet Wacom con pressione e inclinazione, e il firmware KVM hardware ($19 a vita per dispositivo, 24 ore di valutazione gratuita), sono Pro.
</details>

<details>
<summary><b>Posso trasformare un Sipeed NanoKVM o una scheda Radxa in un IP-KVM?</b></summary>

Sì. Flash il firmware USBridge dalla [pagina Hardware Agent](https://www.usbridge.io/hardware-agent/). Ottieni accesso BIOS/UEFI, media virtuali ISO e IMG, snapshot Btrfs immutabili, video a 60 FPS, audio e passthrough USB. Le schede Radxa necessitano di un dongle di cattura HDMI-to-USB.
</details>

<details>
<summary><b>Ho bisogno di port forwarding?</b></summary>

No. Le connessioni avvengono tramite Tailscale peer-to-peer, con attraversamento automatico del NAT.
</details>

<details>
<summary><b>Posso condividere l'accesso a un computer con qualcun altro?</b></summary>

Sì. Invita persone via e-mail con un ruolo e una data di fine; gli ospiti si uniscono tramite USBridge e non necessitano di un account Tailscale.
</details>

## Risorse & Licenza

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Sviluppo & Roadmap</b><br>
      Mantengo un dashboard pubblico per tenere traccia di tutte le funzionalità pianificate, aggiornamenti architettonici e programmi di rilascio.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Visualizza Roadmap Live</a>
    </td>
    <td width="33%" valign="top">
      <b>Link del Progetto</b><br>
      <ul>
        <li><a href="https://usbridge.io">Sito Ufficiale</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Beta Testing & Bugs)</a></li>
        <li><a href="https://www.reddit.com/r/USBridge/">Reddit</a></li>
        <li><a href="https://www.youtube.com/@KVMUSBridge">YouTube</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Supporto Patreon</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>Licenza (GPLv3)</b><br>
      Questo progetto è concesso in licenza sotto la <b>GPLv3</b>. Il client multi-piattaforma incorpora codice da <code>moonlight-common-c</code> (anch'esso GPLv3).<br><br>
      <a href="LICENSE">Visualizza il file LICENSE</a>
    </td>
  </tr>
</table>