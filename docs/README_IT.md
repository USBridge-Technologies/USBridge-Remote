# USBridge Remote (Beta)

![USBridge Remote](https://raw.githubusercontent.com/USBridge-Technologies/USBridge-Remote/main/assets/banner.png)

<div align="center">

[English](README.md) | [Deutsch](docs/README_DE.md) | [Français](docs/README_FR.md) | [Italiano](docs/README_IT.md) | [Español](docs/README_ES.md) | [Português (Brasil)](docs/README_PT_BR.md) | [Українська](docs/README_UA.md) | [Polski](docs/README_PL.md) | [日本語](docs/README_JA.md) | [한국어](docs/README_KO.md) | [简体中文](docs/README_ZH.md)

[![Beta](https://img.shields.io/badge/status-beta-orange)](https://github.com/USBridge-Technologies/USBridge-Remote/releases)
[![Patreon](https://img.shields.io/badge/Patreon-Support_Us-F96854?logo=patreon&logoColor=white)](https://www.patreon.com/USBridge_Technologies)
[![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)](#)
[![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)](#)
[![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)](#)
[![Android](https://img.shields.io/badge/Android-3DDC84?logo=android&logoColor=white)](https://play.google.com/store/apps/details?id=io.usbridge.client)
[![iOS](https://img.shields.io/badge/iOS-000000?logo=apple&logoColor=white)](#)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.com/invite/xqQ6ybkfWS)
<a href="https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?logo=crowdsupply&logoColor=white" alt="Crowd Supply"></a>

</div>

---

**USBridge Remote** è un client unificato ad alte prestazioni per la gestione di macchine remote. L'ho progettato per combinare **accesso al BIOS a livello hardware** (tramite dispositivi USBridge KVM) e **desktop remoto basato su software** in un'unica interfaccia semplificata.

 🖥️ **Hai bisogno di controllo del BIOS a livello hardware prima che il sistema operativo si avvii?**  
 USBridge Remote si integra nativamente con **USBridge-KVM 2.0** per la gestione out-of-band, a livello metallico. 

[![Crowd Supply KVM 2.0](https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?style=for-the-badge&logo=crowdsupply&logoColor=white)](https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)


> ⚠️ **Software Beta** — Questa è una versione preliminare. Aspettati bug. Si prega di segnalare problemi tramite [GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues) o unisciti al nostro [Discord](https://discord.com/invite/xqQ6ybkfWS) per supporto.
> 
> ℹ️ **Nota riguardo ai falsi positivi di Windows Defender / Antivirus:**  
> Windows Defender potrebbe contrassegnare erroneamente `libva.dll` come una minaccia (`Trojan:Win32/Wacatac.B!ml`) a causa di rilevamenti euristici/apprendimento automatico su binari non firmati. **Questo è un falso positivo.**  
> Abbiamo inviato il file a Microsoft Security Intelligence per una revisione ufficiale e l'inserimento nella lista bianca. Nel frattempo, se il tuo antivirus rimuove `libva.dll`, ripristinalo dalla quarantena o aggiungi la cartella USBridge alla lista di esclusione del tuo antivirus.

---

## Download

### Client
Il Client è l'interfaccia di controllo — installata sulla tua workstation o laptop (o eseguita direttamente nel tuo browser). Gestisce le connessioni, il desktop remoto live, il passthrough dei dispositivi virtuali e il registro degli snapshot.

| Architettura | Windows | macOS | Linux | Android | iOS | Web Browser |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Open App](https://web.usbridge.io) |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Download](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Open App](https://web.usbridge.io) |

🌐 **Client Web Zero-Install**: Nessuna installazione richiesta. Basta aprire [web.usbridge.io](https://web.usbridge.io) per connettersi istantaneamente. *(Nota: Il client web opera con alcune limitazioni di funzionalità e prestazioni a causa della sandbox di sicurezza del browser e delle restrizioni di WebRTC. Per un'esperienza completa e senza compromessi, utilizza le app native).*

## Agent

L'Agent viene eseguito sulla macchina di destinazione — il server o il PC a cui desideri accedere in remoto. Gestisce la cattura dello schermo, l'iniezione degli input e la rete Tailscale.

| Architettura | Windows | macOS | Linux |
| :--- | :---: | :---: | :---: |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage) |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg) | — |

---

## Demo

<div align="center">
  <a href="https://youtu.be/1pV9PJeBr7M">
    <img src="https://img.youtube.com/vi/1pV9PJeBr7M/maxresdefault.jpg" alt="USBridge Remote Demo" style="max-width: 100%; border-radius: 8px;">
  </a>
</div>

---

## In the Media

| Media | Evidenza | Link |
| :--- | :--- | :---: |
| **IlSoftware.it** (Italia) | *"USBridge Remote sfida RustDesk e AnyDesk..."* — Recensione approfondita indipendente che elogia la sinergia tra l'agente software e l'hardware KVM, il supporto nativo per Wayland e l'architettura P2P. | [Leggi l'articolo](https://www.ilsoftware.it/alternativa-rustdesk-anydesk-usbridge/) |

---

## Features

<img width="2000" height="1046" alt="USBridge_ap4p" src="https://github.com/user-attachments/assets/2b4bfdf8-412f-4cd7-b4c4-3794d72475cc" />

**Un posto per tutto** — Ho unificato il flusso di lavoro. Gestisci l'hardware USBridge KVM e gli agenti software da un'unica dashboard. Aggiungi una macchina, connettiti e sei dentro.

**Nessun limite, nessun abbonamento** — Completamente gratuito. Nessun limite di tempo di sessione, nessun limite di connessione e nessun account richiesto sulla macchina di destinazione.

**Video a bassa latenza e integrazione Moonlight** — Goditi fino a 2K di risoluzione con 240 FPS fluidi e zero ritardi percepibili. Il mio motore di streaming adattivo sfrutta l'integrazione nativa di Moonlight per offrire prestazioni di desktop remoto senza pari e a latenza ultra-bassa.

**Integrazione Tailscale** — Tunnel P2P crittografati integrati. Connettiti a qualsiasi macchina a livello globale senza dover gestire il port forwarding o le regole del firewall. Funziona automaticamente su LAN e su Internet.

**Appunti condivisi** — Copia e incolla senza problemi tra la tua macchina locale e i target remoti. Supporta completamente testo, immagini e trasferimenti di file out-of-the-box.

**Supporto Multi-Monitor** — Ho aggiunto la possibilità di passare tra più display. Se la macchina di destinazione ha più monitor, ora puoi facilmente selezionare quale visualizzare direttamente dalle impostazioni di connessione. 

<img width="2080" height="1170" alt="Screenshot 2026-05-03 20112н0" src="https://github.com/user-attachments/assets/06dc3de0-2be9-42f7-a897-830a0a6f2bc7" />


---

## Supporto Wayland (Nessun Prompt)

La maggior parte degli agenti di desktop remoto su Linux ha difficoltà con Wayland o ti riempie costantemente di richieste di autorizzazione e popup di conferma ogni volta che inizia una sessione. 

Ho progettato l'Agent USBridge per supportare Wayland nativamente. Gestisce la cattura dello schermo e l'iniezione degli input out-of-the-box **senza alcun fastidioso prompt di autorizzazione** o conferme manuali. Funziona e basta.

---

## Quick Start

1. **Installa l'Agent** sulla macchina a cui desideri accedere in remoto. Avvialo — mostrerà un token di connessione e un indirizzo Tailscale. Connetti Tailscale se hai bisogno di accesso su Internet.

2. **Installa il Client** sulla tua workstation, laptop o telefono.

3. **Aggiungi una connessione** — inserisci l'indirizzo IP o Tailscale mostrato nella finestra dell'Agent. Ecco fatto.

---

##  Project Roadmap

Gestisco i piani di sviluppo software e le funzionalità in arrivo in un dashboard aperto. Se vuoi vedere cosa è attualmente in fase di sviluppo, cosa è pianificato o seguire lo stato delle funzionalità in arrivo, dai un'occhiata alla roadmap live:

 **[Visualizza la Roadmap di USBridge Remote](https://github.com/orgs/USBridge-Technologies/projects/3)**

---

## Community & Beta Testing

Unisciti al nostro Discord per ottenere il ruolo di **Beta Tester**, segnalare bug e aiutarmi a plasmare la roadmap:

**[discord.com/invite/xqQ6ybkfWS](https://discord.com/invite/xqQ6ybkfWS)**

---

## Links

- 🌐 [Sito Ufficiale](https://usbridge.io)
- ❤️ [Pagina Patreon](https://www.patreon.com/USBridge_Technologies)
- 🛒 [USBridge KVM 2.0 su Crowd Supply](https://crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)
- 💬 [Discord](https://discord.com/invite/xqQ6ybkfWS)