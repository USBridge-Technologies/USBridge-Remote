# USBridge Remote

<div align="center">
  <img src="./assets/banner6.svg" width="1400" alt="USBridge Remote">
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

**USBridge Remote** es un cliente unificado de alto rendimiento para gestionar máquinas remotas. Diseñado para combinar **acceso BIOS a nivel de hardware** (a través de dispositivos USBridge KVM) y **escritorio remoto basado en software** en una única interfaz simplificada.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote">
</div>


## Descargar

### Cliente
El Cliente es la interfaz de control — instalada en tu estación de trabajo o laptop (o ejecutada directamente en tu navegador). Gestiona conexiones, escritorio remoto en vivo, paso a través de dispositivos virtuales y registro de instantáneas.

| Arquitectura | Windows | macOS | Linux | Android | iOS | Navegador Web |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Descargar](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Descargar](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Abrir App](https://web.usbridge.io) |
| **ARM64** | — | [Descargar](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Abrir App](https://web.usbridge.io) |

¿Prefieres un APK directo sin una cuenta de Play Store? También se publica una versión autactualizable en la [última versión](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Cliente Web Sin Instalación**: No se requiere instalación. Simplemente abre [web.usbridge.io](https://web.usbridge.io) para conectarte al instante. *(Nota: El cliente web opera con algunas limitaciones de características y rendimiento debido a la seguridad del navegador y las restricciones de WebRTC. Para la experiencia completa sin compromisos, utiliza las aplicaciones nativas).* El cliente web alcanza un Agente a través de su nombre confiable `<label>.device.usbridge.io`: el Agente obtiene ese certificado la primera vez que un navegador accede a él, y solo mientras estés conectado a tu cuenta de USBridge en el Agente — así que el primer intento puede tardar unos minutos. Consulta la fila **Estado → Certificado** del Agente, o la [documentación del Agente](agent/docs/README.md#platform-notes-from-the-top-level-readme) para más detalles. Un USBridge KVM también sirve al cliente web en `https://<kvm>:9443/` y cambia a su propio nombre confiable cuando puede.

## Agente

El Agente se ejecuta en la máquina objetivo — el servidor o PC al que deseas acceder de forma remota. Maneja la captura de pantalla, inyección de entrada y redes Tailscale.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Arquitectura</th>
          <th>Windows</th>
          <th>macOS</th>
          <th>Linux</th>
        </tr>
        <tr>
          <td><b>x86_64</b></td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Descargar</a></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">Descargar</a></td>
        </tr>
        <tr>
          <td><b>ARM64</b></td>
          <td>—</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Descargar</a></td>
          <td>—</td>
        </tr>
      </table>
    </td>
    <!-- Right Column: Image -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="Interfaz del Agente USBridge" width="100%">
    </td>
  </tr>
</table>

## Características

**Codificación acelerada por hardware:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · más software **H.264 (OpenH264)** para VMs y máquinas sin un codificador GPU

<table>
  <tr>
    <td width="33%" valign="top">
      <code>FUNCIONALIDAD BÁSICA</code><br><br>
      <b>SUNSHINE CÓDIGO ABIERTO</b><br>
      <i>Transmisión de Juegos Estándar</i><br><br>
      ✓ Transmisor Sunshine de código abierto incorporado<br>
      ✓ Transmisores alternativos: Vibepollo (fork de Sunshine con Apollo integrado, Windows) y punktfunk (compatible con Moonlight, Windows y Linux), cambia con un clic<br>
      ✓ Benchmark integrado para comparar Sunshine, RustShine, Vibepollo y punktfunk en tu propio hardware y red<br>
      ✓ Acceso de escritorio y juegos de baja latencia<br>
      ✓ Portapapeles de archivos y texto bidireccional<br>    
      ✓ Cambio de pantalla de múltiples monitores<br>
      ✓ Redes P2P Tailscale integradas<br>
      ✓ Acceso de invitados: invita a personas por correo electrónico con un rol y una fecha de finalización<br>
      ✓ Soporte nativo de Wayland (captura sin aviso)
    </td>
    <td width="33%" valign="top">
      <code>+ CARACTERÍSTICAS BÁSICAS</code><br><br>
      <b>USBRIDGE GRATIS</b><br>
      <i>Motor de Transmisión basado en Rust</i><br><br>
      ✓ Protocolo remoto personalizado de conexión instantánea<br>
      ✓ Paso a través de dispositivos periféricos USB en crudo<br>
      ✓ Transmisión adaptativa optimizada para estabilidad Wi-Fi<br>
      ✓ HDR10<br>
      ✓ Precisión de color cromático 4:4:4 sin pérdida<br>
      ✓ Paso a través de micrófono (cliente → host)<br>
      ✓ Paso a través de dispositivo MIDI (cliente → host)<br>
      ✓ Codificación GPU: NVENC, AMF, QSV, VAAPI<br>
      ✓ Software H.264 (OpenH264) para VMs sin GPU<br>
      ✓ Codec de onda de baja latencia PyroWave (por Valve)<br>
      ✓ Gestión de pantalla virtual sin cabeza<br>
      ✓ Paso a través de controladores de gamepad de baja latencia<br>
      ✓ Acceso al cliente del navegador web (sin instalación requerida)<br>
      ✓ Acceso previo al inicio en Windows (entrada de credenciales segura)
    </td>
    <td width="33%" valign="top">
      <code>+ CARACTERÍSTICAS BÁSICAS</code> <code>+ CARACTERÍSTICAS GRATUITAS</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Flujos de Trabajo Profesionales</i><br><br>
      ✓ Soporte para tabletas Wacom con presión y inclinación<br><br>
    </td>
  </tr>
</table>

## Inicio Rápido

1. **Instala el Agente** en la máquina a la que deseas acceder de forma remota. Inícialo — mostrará un token de conexión y una dirección Tailscale. Conecta Tailscale si necesitas acceso a través de Internet.

2. **Instala el Cliente** en tu estación de trabajo, laptop o teléfono.

3. **Agrega una conexión** — ingresa la dirección IP o Tailscale mostrada en la ventana del Agente. Eso es todo.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote">
</div>

## Integración de Hardware

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Control BIOS a Nivel de Hardware</b><br>
      USBridge Remote se integra de manera nativa con el dispositivo USBridge-KVM 2.0 para gestión fuera de banda y a nivel de hardware antes del arranque del SO.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Comprar USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>Firmware IP-KVM DIY</b><br>
      Despliega el firmware oficial en un SBC compatible (por ejemplo, Radxa Zero 3W/3E, Cubie A7) con una interfaz de captura USB para provisionar un nodo KVM personalizado.<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Obtener el Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>Dongle de Hardware USB/IP (ESP32-S3)</b><br>
      El paso a través de USB por software choca con un muro en macOS: no hay un controlador VHCI al que adjuntarse, por lo que una tableta Wacom remota (u otro dispositivo USB) no puede presentarse como hardware real. Este dongle se conecta al Mac y hace en hardware lo que <code>vhci-hcd</code> hace en software en otros lugares — el dispositivo exportado se enumera con su propio VID/PID, vinculado por el propio controlador de macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="firmware y docs de esp32-acm"></a>
    </td>
  </tr>
</table>

## Recursos y Licencia

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Desarrollo y Hoja de Ruta</b><br>
      Mantengo un tablero público para rastrear todas las características planificadas, actualizaciones arquitectónicas y cronogramas de lanzamiento.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Ver Hoja de Ruta en Vivo</a>
    </td>
    <td width="33%" valign="top">
      <b>Enlaces del Proyecto</b><br>
      <ul>
        <li><a href="https://usbridge.io">Sitio Web Oficial</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Pruebas Beta y Errores)</a></li>
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