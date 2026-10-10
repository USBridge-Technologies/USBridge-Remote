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

**USBridge Remote** é um cliente unificado de alto desempenho para gerenciar máquinas remotas. Projetado para combinar **acesso ao BIOS em nível de hardware** (via dispositivos USBridge KVM) e **desktop remoto baseado em software** em uma única interface simplificada.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote">
</div>


## Download

### Cliente
O Cliente é a interface de controle — instalada em sua estação de trabalho ou laptop (ou executada diretamente em seu navegador). Ele gerencia conexões, desktop remoto ao vivo, passthrough de dispositivos virtuais e registro de snapshots.

| Arquitetura | Windows | macOS | Linux | Android | iOS | Navegador Web |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — | [Open App](https://web.usbridge.io) |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | [Open App](https://web.usbridge.io) |

Prefere um APK direto sem uma conta do Play Store? Uma versão autoatualizável também é publicada na [última versão](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

🌐 **Cliente Web Zero-Install**: Nenhuma instalação necessária. Basta abrir [web.usbridge.io](https://web.usbridge.io) para conectar instantaneamente. *(Nota: O cliente web opera com algumas limitações de recursos e desempenho devido à segurança do navegador e restrições do WebRTC. Para a experiência completa, use os aplicativos nativos).* O cliente web alcança um Agente através de seu nome confiável `<label>.device.usbridge.io`: o Agente obtém esse certificado na primeira vez que um navegador acessa, e apenas enquanto você estiver conectado à sua conta USBridge no Agente — então a primeira tentativa pode levar alguns minutos. Veja a linha **Status → Certificado** do Agente, ou a [documentação do Agente](agent/docs/README.md#platform-notes-from-the-top-level-readme) para detalhes. Um USBridge KVM também serve o cliente web em `https://<kvm>:9443/` e muda para seu próprio nome confiável quando pode.

## Agente

O Agente é executado na máquina alvo — o servidor ou PC que você deseja acessar remotamente. Ele gerencia captura de tela, injeção de entrada e rede Tailscale.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Arquitetura</th>
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
      <img src="./assets/agent-screenshot.svg" alt="USBridge Agent Interface" width="100%">
    </td>
  </tr>
</table>

## Recursos

<table>
  <tr>
    <td width="33%" valign="top">
      <code>FUNCIONALIDADE BÁSICA</code><br><br>
      <b>SUNSHINE CÓDIGO ABERTO</b><br>
      <i>Streaming de Jogos Padrão</i><br><br>
      ✓ Streamer Sunshine de código aberto embutido<br>
      ✓ Acesso a desktop e jogos com baixa latência<br>
      ✓ Área de transferência de arquivos e texto bidirecional<br>    
      ✓ Troca de exibição em múltiplos monitores<br>
      ✓ Rede P2P Tailscale integrada<br>
      ✓ Suporte nativo a Wayland (captura sem prompt)
    </td>
    <td width="33%" valign="top">
      <code>+ RECURSOS BÁSICOS</code><br><br>
      <b>USBRIDGE GRÁTIS</b><br>
      <i>Motor de Stream baseado em Rust</i><br><br>
      ✓ Protocolo remoto personalizado de conexão instantânea<br>
      ✓ Passthrough de dispositivo periférico USB bruto<br>
      ✓ Streaming adaptativo otimizado para estabilidade Wi-Fi<br>
      ✓ HDR10<br>
      ✓ Gerenciamento de exibição virtual sem cabeça<br>
      ✓ Passthrough de controle de gamepad com baixa latência<br>
      ✓ Acesso ao cliente do navegador web (sem instalação necessária)<br>
      ✓ Acesso pré-login no Windows (entrada segura de credenciais)
    </td>
    <td width="33%" valign="top">
      <code>+ RECURSOS BÁSICOS</code> <code>+ RECURSOS GRÁTIS</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Fluxos de Trabalho Profissionais</i><br><br>
      ✓ Precisão de cor de croma 4:4:4 sem perdas<br>
      ✓ Suporte a tablet Wacom com pressão e inclinação<br><br>
    </td>
  </tr>
</table>

## Início Rápido

1. **Instale o Agente** na máquina que você deseja acessar remotamente. Inicie-o — ele exibirá um token de conexão e um endereço Tailscale. Conecte-se ao Tailscale se precisar de acesso pela internet.

2. **Instale o Cliente** em sua estação de trabalho, laptop ou telefone.

3. **Adicione uma conexão** — insira o IP ou o endereço Tailscale mostrado na janela do Agente. É isso.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote">
</div>

## Integração de Hardware

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Controle de BIOS em Nível de Hardware</b><br>
      O USBridge Remote integra-se nativamente com o dispositivo USBridge-KVM 2.0 para gerenciamento fora de banda, bare-metal antes da inicialização do SO.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>Firmware IP-KVM DIY</b><br>
      Implemente o firmware oficial em um SBC compatível (por exemplo, Radxa Zero 3W/3E, Cubie A7) com uma interface de captura USB para provisionar um nó KVM personalizado.<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Get the Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>Dongle de Hardware USB/IP (ESP32-S3)</b><br>
      O passthrough de USB por software encontra um obstáculo no macOS: não há driver VHCI para se conectar, então um tablet Wacom remoto (ou outro dispositivo USB) não pode ser apresentado como hardware real. Este dongle se conecta ao Mac e faz em hardware o que <code>vhci-hcd</code> faz em software em outros lugares — o dispositivo exportado é enumerado com seu próprio VID/PID, vinculado pelo próprio driver do macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm firmware & docs"></a>
    </td>
  </tr>
</table>

## Recursos & Licença

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Desenvolvimento & Roteiro</b><br>
      Eu mantenho um painel público para acompanhar todos os recursos planejados, atualizações arquitetônicas e cronogramas de lançamento.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Ver Roteiro Ao Vivo</a>
    </td>
    <td width="33%" valign="top">
      <b>Links do Projeto</b><br>
      <ul>
        <li><a href="https://usbridge.io">Site Oficial</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Teste Beta & Bugs)</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Apoio no Patreon</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>Licença (GPLv3)</b><br>
      Este projeto é licenciado sob a <b>GPLv3</b>. O cliente multiplataforma incorpora código da <code>moonlight-common-c</code> (também GPLv3).<br><br>
      <a href="LICENSE">Ver Arquivo de LICENÇA</a>
    </td>
  </tr>
</table>