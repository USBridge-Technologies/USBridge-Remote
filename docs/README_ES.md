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

**USBridge Remote** es un cliente unificado de alto rendimiento para gestionar máquinas remotas. Lo diseñé para combinar el **acceso a BIOS a nivel de hardware** (a través de dispositivos USBridge KVM) y **escritorio remoto basado en software** en una única interfaz simplificada.

 🖥️ **¿Necesitas control de BIOS a nivel de hardware antes de que arranque el sistema operativo?**  
 USBridge Remote se integra de forma nativa con **USBridge-KVM 2.0** para gestión fuera de banda a nivel de metal.

[![Crowd Supply KVM 2.0](https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?style=for-the-badge&logo=crowdsupply&logoColor=white)](https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)


> ⚠️ **Software Beta** — Esta es una versión temprana. Espera errores. Por favor, informa sobre problemas a través de [GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues) o únete a nuestro [Discord](https://discord.com/invite/xqQ6ybkfWS) para soporte.
> 
> ℹ️ **Nota sobre falsos positivos de Windows Defender / Antivirus:**  
> Windows Defender puede marcar incorrectamente `libva.dll` como una amenaza (`Trojan:Win32/Wacatac.B!ml`) debido a la detección heurística/aprendizaje automático en binarios no firmados. **Esto es un falso positivo.**  
> Hemos enviado el archivo a Microsoft Security Intelligence para revisión oficial y lista blanca. Mientras tanto, si tu antivirus elimina `libva.dll`, por favor, restáuralo desde Cuarentena o añade la carpeta USBridge a la lista de exclusión de tu antivirus.

---

## Descargar

### Cliente
El Cliente es la interfaz de control — instalada en tu estación de trabajo o laptop (o ejecutada directamente en tu navegador). Gestiona conexiones, escritorio remoto en vivo, paso de dispositivos virtuales y registro de instantáneas.

| Arquitectura | Windows | macOS | Linux | Android | iOS | Navegador Web |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Descargar](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Descargar](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Abrir App](https://web.usbridge.io) |
| **ARM64** | — | [Descargar](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Abrir App](https://web.usbridge.io) |

¿Prefieres un APK directo sin una cuenta de Play Store? También se publica una versión autoactualizable en la [última versión](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Cliente Web Sin Instalación**: No se requiere instalación. Simplemente abre [web.usbridge.io](https://web.usbridge.io) para conectarte al instante. *(Nota: El cliente web opera con algunas limitaciones de características y rendimiento debido a la seguridad del navegador y las restricciones de WebRTC. Para la experiencia completa sin compromisos, utiliza las aplicaciones nativas).*

## Agente

El Agente se ejecuta en la máquina objetivo — el servidor o PC al que deseas acceder de forma remota. Maneja la captura de pantalla, inyección de entrada y redes Tailscale.

| Arquitectura | Windows | macOS | Linux |
| :--- | :---: | :---: | :---: |
| **x86_64** | [Descargar](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip) | — | [Descargar](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage) |
| **ARM64** | — | [Descargar](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg) | — |

---

## Demo

<div align="center">
  <a href="https://youtu.be/1pV9PJeBr7M">
    <img src="https://img.youtube.com/vi/1pV9PJeBr7M/maxresdefault.jpg" alt="USBridge Remote Demo" style="max-width: 100%; border-radius: 8px;">
  </a>
</div>

---

## Características

<img width="2000" height="1046" alt="USBridge_ap4p" src="https://github.com/user-attachments/assets/2b4bfdf8-412f-4cd7-b4c4-3794d72475cc" />

**Un lugar para todo** — He unificado el flujo de trabajo. Gestiona el hardware USBridge KVM y los agentes de software desde un único panel de control. Agrega una máquina, conéctate y ya estás dentro.

**Sin límites, sin suscripciones** — Completamente gratis. Sin límites de tiempo de sesión, sin límites de conexión y sin cuenta requerida en la máquina objetivo.

**Video de baja latencia e integración con Moonlight** — Disfruta de hasta 2K de resolución con 240 FPS suaves como la mantequilla y cero retraso perceptible. Mi motor de transmisión adaptativa aprovecha la integración nativa de Moonlight para ofrecer un rendimiento de escritorio remoto inigualable y de ultra baja latencia.

**Integración con Tailscale** — Túneles P2P cifrados integrados. Conéctate a cualquier máquina a nivel global sin complicarte con el reenvío de puertos o reglas de firewall. Funciona en LAN y a través de internet automáticamente.

**Portapapeles Compartido** — Copia y pega sin problemas entre tu máquina local y los objetivos remotos. Soporta completamente texto, imágenes y transferencias de archivos desde el primer momento.

**Soporte para Múltiples Monitores** — He añadido la capacidad de cambiar entre múltiples pantallas. Si la máquina objetivo tiene varios monitores, ahora puedes seleccionar fácilmente cuál ver directamente desde la configuración de conexión. 

<img width="2080" height="1170" alt="Screenshot 2026-05-03 20112н0" src="https://github.com/user-attachments/assets/06dc3de0-2be9-42f7-a897-830a0a6f2bc7" />


---

## Soporte para Wayland (Sin Prompts)

La mayoría de los agentes de escritorio remoto en Linux tienen problemas con Wayland o te bombardean constantemente con solicitudes de permiso y ventanas emergentes de confirmación cada vez que comienza una sesión. 

Diseñé el Agente USBridge para soportar Wayland de forma nativa. Maneja la captura de pantalla completa y la inyección de entrada desde el primer momento **sin ninguna molesta solicitud de permiso** o confirmaciones manuales. Simplemente funciona.

---

## Inicio Rápido

1. **Instala el Agente** en la máquina a la que deseas acceder de forma remota. Inícialo — mostrará un token de conexión y una dirección de Tailscale. Conéctate a Tailscale si necesitas acceso a través de internet.

2. **Instala el Cliente** en tu estación de trabajo, laptop o teléfono.

3. **Agrega una conexión** — ingresa la dirección IP o la dirección de Tailscale mostrada en la ventana del Agente. Eso es todo.

---

## Hoja de Ruta del Proyecto

Gestiono los planes de desarrollo de software y las características próximas en un panel abierto. Si deseas ver qué se está desarrollando actualmente, qué está planeado o seguir el estado de las características próximas, consulta la hoja de ruta en vivo:

 **[Ver Hoja de Ruta de USBridge Remote](https://github.com/orgs/USBridge-Technologies/projects/3)**

---

## Comunidad y Pruebas Beta

Únete a nuestro Discord para obtener el rol de **Beta Tester**, informar sobre errores y ayudarme a dar forma a la hoja de ruta:

**[discord.com/invite/xqQ6ybkfWS](https://discord.com/invite/xqQ6ybkfWS)**

---

## Enlaces

- 🌐 [Sitio Web Oficial](https://usbridge.io)
- ❤️ [Página de Patreon](https://www.patreon.com/USBridge_Technologies)
- 🛒 [USBridge KVM 2.0 en Crowd Supply](https://crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)
- 💬 [Discord](https://discord.com/invite/xqQ6ybkfWS)

---

## 📜 Licencia

Este proyecto está licenciado bajo **GPLv3** (ver [`LICENSE`](LICENSE)). El cliente de Android/Windows/macOS/Linux incorpora código de `moonlight-common-c` (también GPLv3).