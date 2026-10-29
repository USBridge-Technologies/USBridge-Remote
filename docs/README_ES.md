# USBridge Remote

<div align="center">
  <img src="./assets/banner6.svg" width="1400" alt="USBridge Remote: escritorio remoto de baja latencia y KVM sobre IP">
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

**USBridge Remote** es un ecosistema de **escritorio remoto** y **KVM sobre IP** de baja latencia: un cliente para Windows, macOS, Linux, Android, iOS y el navegador, un agente host con un streamer de juegos **Sunshine / Moonlight** incorporado, y firmware IP-KVM para las placas **Radxa Zero 3W**, **Radxa Cubie A7Z** y **Sipeed NanoKVM**. Combina **acceso BIOS a nivel de hardware** y **escritorio remoto de software** en una sola interfaz, con passthrough USB para gamepads, tabletas Wacom, MIDI y micrófonos, y redes P2P Tailscale sin necesidad de reenvío de puertos.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="Características de USBridge Remote: escritorio remoto, transmisión de juegos, passthrough USB, KVM de hardware">
</div>


## Descargar

**Instalador todo en uno (Cliente + Agente, elige uno o ambos durante la configuración):** [Windows Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) · [imagen de disco de macOS (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-macOS-arm64.dmg)

Los paquetes de Linux (.deb, .rpm, Arch) añaden el repositorio de paquetes de USBridge, por lo que las aplicaciones se actualizan con el resto del sistema. Todos los archivos están en la [última versión](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

### Cliente
El Cliente es la interfaz de control — instalada en tu estación de trabajo o laptop (o ejecutada directamente en tu navegador). Gestiona conexiones, escritorio remoto en vivo, passthrough de dispositivos virtuales y registro de instantáneas.

| Plataforma | Instalador | Portátil | Paquetes / Tienda |
| :--- | :--- | :--- | :--- |
| **Windows** x86_64 | [Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) | [Portable (.zip)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — |
| **macOS** Apple Silicon | [Imagen de disco (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | — |
| **Linux** x86_64 | [Debian / Ubuntu (.deb)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.deb) | [AppImage](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | [Fedora / RHEL (.rpm)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.rpm) · [Arch (.pkg.tar.zst)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.pkg.tar.zst) |
| **Android** ARM64 | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [APK (auto-actualizable)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-selfupdate.apk) | — |
| **iOS / iPadOS** | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | — | — |
| **Navegador web** | — | [Abrir web.usbridge.io](https://web.usbridge.io) | — |

🌐 **Cliente Web sin instalación**: No se requiere instalación. Solo abre [web.usbridge.io](https://web.usbridge.io) para conectarte al instante. *(Nota: El cliente web opera con algunas limitaciones de características y rendimiento debido a la seguridad del navegador y las restricciones de WebRTC. Para una experiencia completa sin compromisos, utiliza las aplicaciones nativas).* El cliente web llega a un Agente a través de su nombre confiable `<label>.device.usbridge.io`: el Agente obtiene ese certificado la primera vez que un navegador se conecta, y solo mientras estés conectado a tu cuenta de USBridge en el Agente — por lo que el primer intento puede tardar unos minutos. Consulta la fila **Estado → Certificado** del Agente, o la [documentación del Agente](agent/docs/README.md#platform-notes-from-the-top-level-readme) para más detalles. Un KVM de USBridge también sirve al cliente web en `https://<kvm>:9443/` y cambia a su propio nombre confiable cuando puede.

## Agente

El Agente se ejecuta en la máquina objetivo — el servidor o PC al que deseas acceder de forma remota. Maneja la captura de pantalla, inyección de entrada y redes Tailscale.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Plataforma</th>
          <th>Descargas</th>
        </tr>
        <tr>
          <td><b>Windows</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64-Setup.exe">Setup (.exe)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Portable (.zip)</a></td>
        </tr>
        <tr>
          <td><b>macOS</b><br>Apple Silicon</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Imagen de disco (.dmg)</a></td>
        </tr>
        <tr>
          <td><b>Linux</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.deb">Debian / Ubuntu (.deb)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.rpm">Fedora / RHEL (.rpm)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.pkg.tar.zst">Arch (.pkg.tar.zst)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">AppImage (portátil)</a></td>
        </tr>
      </table>
    </td>
    <!-- Right Column: Image -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="Ventana del Agente USBridge: token de conexión, dirección Tailscale y estado de transmisión" width="100%">
    </td>
  </tr>
</table>

## Características

**Codificación acelerada por hardware:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · además de software **H.264 (OpenH264)** para máquinas virtuales y máquinas sin un codificador GPU

<table>
  <tr>
    <td width="33%" valign="top">
      <code>FUNCIONALIDAD BÁSICA</code><br><br>
      <b>SUNSHINE CÓDIGO ABIERTO</b><br>
      <i>Transmisión de Juegos Estándar</i><br><br>
      ✓ Streamer Sunshine de código abierto incorporado<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">Streamers alternativos</a>: Vibepollo (fork de Sunshine con Apollo incorporado, Windows) y punktfunk (compatible con Moonlight, Windows y Linux), cambiar con un clic<br>
      ✓ Benchmark incorporado para comparar Sunshine, RustShine, Vibepollo y punktfunk en tu propio hardware y red<br>
      ✓ Acceso de escritorio y juegos de baja latencia<br>
      ✓ Portapapeles de archivos y texto bidireccional<br>    
      ✓ Cambio de visualización de múltiples monitores<br>
      ✓ Redes P2P Tailscale integradas<br>
      ✓ Acceso de invitados: invita a personas por correo electrónico con un rol y una fecha de finalización<br>
      ✓ Soporte nativo de Wayland (captura sin aviso)
    </td>
    <td width="33%" valign="top">
      <code>+ CARACTERÍSTICAS BÁSICAS</code><br><br>
      <b>USBRIDGE GRATIS</b><br>
      <i>Motor de Transmisión basado en Rust</i><br><br>
      ✓ Protocolo remoto personalizado de conexión instantánea<br>
      ✓ Passthrough de dispositivo periférico USB <a href="https://www.usbridge.io/usb-passthrough/">en crudo</a><br>
      ✓ Transmisión adaptativa optimizada para estabilidad Wi-Fi<br>
      ✓ HDR10<br>
      ✓ Precisión de color 4:4:4 sin pérdida<br>
      ✓ Passthrough de micrófono (cliente → host)<br>
      ✓ Passthrough de dispositivo MIDI (cliente → host)<br>
      ✓ Codificación GPU: NVENC, AMF, QSV, VAAPI<br>
      ✓ Software H.264 (OpenH264) para máquinas virtuales sin GPU<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> códec de wavelet de baja latencia (por Valve)<br>
      ✓ Gestión de visualización virtual sin cabeza<br>
      ✓ Passthrough de controlador de gamepad de baja latencia<br>
      ✓ Acceso al cliente del navegador web (sin instalación requerida)<br>
      ✓ Acceso previo al inicio de sesión en Windows (entrada de credenciales segura)
    </td>
    <td width="33%" valign="top">
      <code>+ CARACTERÍSTICAS BÁSICAS</code> <code>+ CARACTERÍSTICAS GRATIS</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Flujos de Trabajo Profesionales</i><br><br>
      ✓ Soporte para tabletas Wacom con presión y inclinación (<a href="https://www.usbridge.io/macos-wacom-support/">también en macOS</a>)<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">Firmware KVM de hardware</a> para Radxa Zero 3W, Radxa Cubie A7Z y Sipeed NanoKVM: acceso a BIOS/UEFI, medios virtuales ISO, instantáneas Btrfs ($19 de por vida por dispositivo, evaluación gratuita de 24 h)<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">Streamer Moonlight de hardware</a> en esas placas: cualquier fuente HDMI (PC, PS5, Xbox, Switch) a Moonlight a 1080p 60 FPS, sin software en la fuente<br><br>
    </td>
  </tr>
</table>

## Inicio Rápido

1. **Instala el Agente** en la máquina a la que deseas acceder de forma remota. Inícialo — mostrará un token de conexión y una dirección Tailscale. Conéctate a Tailscale si necesitas acceso a través de Internet.

2. **Instala el Cliente** en tu estación de trabajo, laptop o teléfono.

3. **Agrega una conexión** — ingresa la dirección IP o Tailscale mostrada en la ventana del Agente. Eso es todo.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="Inicio rápido de USBridge Remote: instala el agente, instala el cliente, conéctate">
</div>

## Integración de Hardware

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Control BIOS a Nivel de Hardware</b><br>
      USBridge Remote se integra de manera nativa con el dispositivo USBridge-KVM 2.0 para gestión fuera de banda y bare-metal antes del arranque del SO. También es el primer streamer Moonlight de hardware: el cliente Moonlight estándar se empareja con el KVM y transmite cualquier fuente HDMI (un PC o una consola de juegos) sin software en ella.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Comprar USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>Firmware IP-KVM DIY</b><br>
      Flashea el firmware oficial en una placa compatible (Radxa Zero 3W, Radxa Cubie A7Z) con un dongle de captura HDMI a USB para construir tu propio IP-KVM. En un <b>Sipeed NanoKVM</b> reemplaza el firmware de stock: video suave a 60 FPS en lugar de una imagen entrecortada, audio y micrófono, gamepads, passthrough USB y soporte Moonlight (<a href="https://www.usbridge.io/hardware-agent/">descargar el firmware</a>).<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Obtener el Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>Dongle de Hardware USB/IP (ESP32-S3)</b><br>
      El passthrough USB de software tiene un límite en macOS: no hay un controlador VHCI al que conectarse, por lo que una tableta Wacom remota (u otro dispositivo USB) no puede presentarse como hardware real. Este dongle se conecta al Mac y hace en hardware lo que <code>vhci-hcd</code> hace en software en otros lugares — el dispositivo exportado se enumera con su propio VID/PID, vinculado por el controlador propio de macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="firmware y docs esp32-acm"></a>
    </td>
  </tr>
</table>

### NanoKVM con firmware USBridge: 60 FPS en lugar de retraso

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Video: Sipeed NanoKVM flasheado con firmware USBridge transmite a 60 FPS con casi ningún retraso de entrada"></a>
  <br><br>
  <img src="https://img.shields.io/badge/1080p-60_FPS-2da44e?style=flat-square" alt="1080p 60 FPS">
  <img src="https://img.shields.io/badge/Tailscale-P2P-242424?style=flat-square&logo=tailscale&logoColor=white" alt="Tailscale P2P">
  <img src="https://img.shields.io/badge/Audio-sonido_%2B_mic-007ec6?style=flat-square" alt="Audio: sonido y micrófono">
  <img src="https://img.shields.io/badge/FEC-recuperación_de_pérdida_de_paquetes-f59e0b?style=flat-square" alt="FEC: corrección de errores hacia adelante">
</div>

Un video de 2 minutos: un Sipeed NanoKVM con un gran retraso en un laboratorio en casa se convierte en un KVM sobre IP a 60 FPS con casi ningún retraso de entrada después de flashear el firmware USBridge, lo suficientemente rápido como para jugar juegos con un gamepad. Tailscale funciona en la placa misma, por lo que es accesible desde cualquier lugar sin reenvío de puertos; el sonido se reproduce en ambas direcciones, y FEC (corrección de errores hacia adelante) reconstruye paquetes perdidos, por lo que la imagen se mantiene limpia en Wi-Fi y a través de Internet. El firmware para el NanoKVM, Radxa Zero 3W y Radxa Cubie A7Z está en la [página del Agente de Hardware](https://www.usbridge.io/hardware-agent/).

## FAQ

<details>
<summary><b>¿Funciona USBridge con Moonlight y Sunshine?</b></summary>

Sí. El Agente incluye el streamer Sunshine de código abierto, por lo que el cliente Moonlight estándar se conecta a él. Vibepollo (un fork de Sunshine con Apollo incorporado) y punktfunk también están incorporados y cambian con un clic.
</details>

<details>
<summary><b>¿Hay un streamer Moonlight de hardware?</b></summary>

Sí. Un USBridge KVM 2.0, o un Radxa Zero 3W, Radxa Cubie A7Z o Sipeed NanoKVM flasheado con firmware USBridge, es un host GameStream compatible con Sunshine para cualquier cosa en su entrada HDMI: un PC, PS5, Xbox o Switch. Transmite a 1080p 60 FPS con audio al cliente Moonlight estándar, sin software en la fuente. Consulta [Streamer Moonlight de Hardware](https://www.usbridge.io/moonlight-hardware-streamer/).
</details>

<details>
<summary><b>¿Cuál streamer es más rápido en mi PC?</b></summary>

Depende de la GPU, el controlador y la red. El benchmark incorporado ejecuta Sunshine, RustShine, Vibepollo y punktfunk en tu propio hardware y red y los compara. Consulta [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/).
</details>

<details>
<summary><b>¿Qué GPUs son compatibles para codificación?</b></summary>

NVIDIA NVENC, AMD AMF, Intel QSV y VAAPI en Linux. Las máquinas virtuales y PCs sin un codificador GPU utilizan software H.264 (OpenH264).
</details>

<details>
<summary><b>¿Qué es gratis y qué necesita Pro?</b></summary>

Color 4:4:4, HDR10, PyroWave, gamepads, micrófono, MIDI y passthrough USB en crudo son gratis. Las tabletas Wacom con presión e inclinación, y el firmware KVM de hardware ($19 de por vida por dispositivo, evaluación gratuita de 24 h), son Pro.
</details>

<details>
<summary><b>¿Puedo convertir un Sipeed NanoKVM o una placa Radxa en un IP-KVM?</b></summary>

Sí. Flashea el firmware USBridge desde la [página del Agente de Hardware](https://www.usbridge.io/hardware-agent/). Obtienes acceso a BIOS/UEFI, medios virtuales ISO e IMG, instantáneas Btrfs inmutables, video a 60 FPS, audio y passthrough USB. Las placas Radxa necesitan un dongle de captura HDMI a USB.
</details>

<details>
<summary><b>¿Necesito reenvío de puertos?</b></summary>

No. Las conexiones se realizan a través de Tailscale de igual a igual, con recorrido NAT automático.
</details>

<details>
<summary><b>¿Puedo compartir el acceso a una computadora con otra persona?</b></summary>

Sí. Invita a personas por correo electrónico con un rol y una fecha de finalización; los invitados se unen a través de USBridge y no necesitan una cuenta de Tailscale.
</details>

## Recursos y Licencia

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Desarrollo y Hoja de Ruta</b><br>
      Mantengo un panel público para rastrear todas las características planificadas, actualizaciones arquitectónicas y cronogramas de lanzamiento.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Ver Hoja de Ruta en Vivo</a>
    </td>
    <td width="33%" valign="top">
      <b>Enlaces del Proyecto</b><br>
      <ul>
        <li><a href="https://usbridge.io">Sitio Web Oficial</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Pruebas Beta y Errores)</a></li>
        <li><a href="https://www.reddit.com/r/USBridge/">Reddit</a></li>
        <li><a href="https://www.youtube.com/@KVMUSBridge">YouTube</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Soporte en Patreon</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>Licencia (GPLv3)</b><br>
      Este proyecto está licenciado bajo la <b>GPLv3</b>. El cliente multiplataforma incorpora código de <code>moonlight-common-c</code> (también GPLv3).<br><br>
      <a href="LICENSE">Ver Archivo de LICENCIA</a>
    </td>
  </tr>
</table>