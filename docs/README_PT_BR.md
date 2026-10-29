# USBridge Remote

<div align="center">
  <img src="./assets/banner6.svg" width="1400" alt="USBridge Remote: desktop remoto de baixa latência e KVM-over-IP">
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

**USBridge Remote** é um ecossistema de **desktop remoto** de baixa latência e **KVM-over-IP**: um cliente para Windows, macOS, Linux, Android, iOS e o navegador, um agente host com um streamer de jogos **Sunshine / Moonlight** embutido, e firmware IP-KVM para placas **Radxa Zero 3W**, **Radxa Cubie A7Z** e **Sipeed NanoKVM**. Ele combina **acesso ao BIOS em nível de hardware** e **desktop remoto de software** em uma única interface, com passthrough USB para gamepads, tablets Wacom, MIDI e microfones, e rede P2P Tailscale sem necessidade de redirecionamento de portas.

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="Recursos do USBridge Remote: desktop remoto, streaming de jogos, passthrough USB, KVM de hardware">
</div>


## Download

**Instalador tudo-em-um (Cliente + Agente, escolha um ou ambos durante a configuração):** [Windows Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) · [imagem de disco macOS (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-macOS-arm64.dmg)

Os pacotes Linux (.deb, .rpm, Arch) adicionam o repositório de pacotes USBridge, então os aplicativos se atualizam com o restante do sistema. Todos os arquivos estão na [última versão](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest).

### Cliente
O Cliente é a interface de controle — instalada em sua estação de trabalho ou laptop (ou executada diretamente em seu navegador). Ele gerencia conexões, desktop remoto ao vivo, passthrough de dispositivos virtuais e registro de instantâneas.

| Plataforma | Instalador | Portátil | Pacotes / Loja |
| :--- | :--- | :--- | :--- |
| **Windows** x86_64 | [Setup (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) | [Portátil (.zip)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — |
| **macOS** Apple Silicon | [Imagem de disco (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | — |
| **Linux** x86_64 | [Debian / Ubuntu (.deb)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.deb) | [AppImage](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | [Fedora / RHEL (.rpm)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.rpm) · [Arch (.pkg.tar.zst)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.pkg.tar.zst) |
| **Android** ARM64 | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) | [APK (autoatualizável)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-selfupdate.apk) | — |
| **iOS / iPadOS** | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) | — | — |
| **Navegador web** | — | [Abrir web.usbridge.io](https://web.usbridge.io) | — |

🌐 **Cliente Web Zero-Install**: Nenhuma instalação necessária. Basta abrir [web.usbridge.io](https://web.usbridge.io) para conectar instantaneamente. *(Nota: O cliente web opera com algumas limitações de recursos e desempenho devido à segurança do navegador e restrições do WebRTC. Para a experiência completa e sem compromissos, use os aplicativos nativos).* O cliente web alcança um Agente através de seu nome confiável `<label>.device.usbridge.io`: o Agente obtém esse certificado na primeira vez que um navegador acessa, e apenas enquanto você estiver conectado à sua conta USBridge no Agente — então a primeira tentativa pode levar alguns minutos. Veja a linha **Status → Certificado** do Agente, ou a [documentação do Agente](agent/docs/README.md#platform-notes-from-the-top-level-readme) para detalhes. Um KVM USBridge também serve o próprio cliente web em `https://<kvm>:9443/` e muda para seu próprio nome confiável quando pode.

## Agente

O Agente é executado na máquina alvo — o servidor ou PC que você deseja acessar remotamente. Ele lida com captura de tela, injeção de entrada e rede Tailscale.

<table>
  <tr>
    <!-- Left Column: Downloads Table -->
    <td valign="middle">
      <table>
        <tr>
          <th>Plataforma</th>
          <th>Downloads</th>
        </tr>
        <tr>
          <td><b>Windows</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64-Setup.exe">Setup (.exe)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">Portátil (.zip)</a></td>
        </tr>
        <tr>
          <td><b>macOS</b><br>Apple Silicon</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">Imagem de disco (.dmg)</a></td>
        </tr>
        <tr>
          <td><b>Linux</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.deb">Debian / Ubuntu (.deb)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.rpm">Fedora / RHEL (.rpm)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.pkg.tar.zst">Arch (.pkg.tar.zst)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">AppImage (portátil)</a></td>
        </tr>
      </table>
    </td>
    <!-- Right Column: Image -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="Janela do Agente USBridge: token de conexão, endereço Tailscale e status de streaming" width="100%">
    </td>
  </tr>
</table>

## Recursos

**Codificação acelerada por hardware:** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · além de software **H.264 (OpenH264)** para VMs e máquinas sem um codificador GPU

<table>
  <tr>
    <td width="33%" valign="top">
      <code>FUNCIONALIDADE BÁSICA</code><br><br>
      <b>SUNSHINE CÓDIGO ABERTO</b><br>
      <i>Streaming de Jogos Padrão</i><br><br>
      ✓ Streamer Sunshine de código aberto embutido<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">Streamers alternativos</a>: Vibepollo (fork do Sunshine com Apollo embutido, Windows) e punktfunk (compatível com Moonlight, Windows e Linux), alternar em um clique<br>
      ✓ Benchmark embutido para comparar Sunshine, RustShine, Vibepollo e punktfunk em seu próprio hardware e rede<br>
      ✓ Acesso a desktop e jogos de baixa latência<br>
      ✓ Área de transferência de arquivos e texto bidirecional<br>    
      ✓ Alternância de exibição de múltiplos monitores<br>
      ✓ Rede P2P Tailscale integrada<br>
      ✓ Acesso de convidado: convide pessoas por e-mail com um papel e uma data de término<br>
      ✓ Suporte nativo ao Wayland (captura sem prompt)
    </td>
    <td width="33%" valign="top">
      <code>+ RECURSOS BÁSICOS</code><br><br>
      <b>USBRIDGE GRATUITO</b><br>
      <i>Motor de Streaming baseado em Rust</i><br><br>
      ✓ Protocolo remoto personalizado de conexão instantânea<br>
      ✓ Passthrough de dispositivo periférico USB <a href="https://www.usbridge.io/usb-passthrough/">bruto</a><br>
      ✓ Streaming adaptativo otimizado para estabilidade Wi-Fi<br>
      ✓ HDR10<br>
      ✓ Precisão de cor 4:4:4 sem perdas<br>
      ✓ Passthrough de microfone (cliente → host)<br>
      ✓ Passthrough de dispositivo MIDI (cliente → host)<br>
      ✓ Codificação GPU: NVENC, AMF, QSV, VAAPI<br>
      ✓ Software H.264 (OpenH264) para VMs sem GPU<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> codec de wavelet de baixa latência (da Valve)<br>
      ✓ Gerenciamento de exibição virtual sem cabeça<br>
      ✓ Passthrough de controlador de gamepad de baixa latência<br>
      ✓ Acesso ao cliente do navegador web (nenhuma instalação necessária)<br>
      ✓ Acesso pré-login no Windows (entrada de credenciais segura)
    </td>
    <td width="33%" valign="top">
      <code>+ RECURSOS BÁSICOS</code> <code>+ RECURSOS GRATUITOS</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>Fluxos de Trabalho Profissionais</i><br><br>
      ✓ Suporte a tablet Wacom com pressão e inclinação (<a href="https://www.usbridge.io/macos-wacom-support/">macOS também</a>)<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">Firmware KVM de hardware</a> para Radxa Zero 3W, Radxa Cubie A7Z e Sipeed NanoKVM: acesso ao BIOS/UEFI, mídia virtual ISO, instantâneas Btrfs ($19 vitalício por dispositivo, avaliação gratuita de 24h)<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">Streamer Moonlight de hardware</a> nessas placas: qualquer fonte HDMI (PC, PS5, Xbox, Switch) para Moonlight a 1080p 60 FPS, sem software na fonte<br><br>
    </td>
  </tr>
</table>

## Início Rápido

1. **Instale o Agente** na máquina que você deseja acessar remotamente. Inicie-o — ele exibirá um token de conexão e um endereço Tailscale. Conecte-se ao Tailscale se precisar de acesso pela internet.

2. **Instale o Cliente** em sua estação de trabalho, laptop ou telefone.

3. **Adicione uma conexão** — insira o IP ou o endereço Tailscale mostrado na janela do Agente. É isso.

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="Início rápido do USBridge Remote: instale o agente, instale o cliente, conecte-se">
</div>

## Integração de Hardware

<table>
  <tr>
   <td width="33%" valign="top">
      <b>Controle de BIOS em Nível de Hardware</b><br>
      O USBridge Remote integra-se nativamente com o dispositivo USBridge-KVM 2.0 para gerenciamento fora da banda, bare-metal antes da inicialização do SO. É também o primeiro streamer Moonlight de hardware: o cliente Moonlight padrão emparelha-se com o KVM e transmite qualquer fonte HDMI (um PC ou um console de jogos) sem software nele.<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Comprar USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>Firmware IP-KVM DIY</b><br>
      Flash o firmware oficial em uma placa compatível (Radxa Zero 3W, Radxa Cubie A7Z) com um dongle de captura HDMI-para-USB para construir seu próprio IP-KVM. Em um <b>Sipeed NanoKVM</b>, ele substitui o firmware padrão: vídeo suave a 60 FPS em vez de uma imagem tremida, áudio e microfone, gamepads, passthrough USB e suporte a Moonlight (<a href="https://www.usbridge.io/hardware-agent/">baixe o firmware</a>).<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-FIRMWARE_DIY-007ec6?style=for-the-badge" alt="Obter o Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>Dongle de Hardware USB/IP (ESP32-S3)</b><br>
      O passthrough USB de software encontra um limite no macOS: não há driver VHCI para se conectar, então um tablet Wacom remoto (ou outro dispositivo USB) não pode ser apresentado como hardware real. Este dongle se conecta ao Mac e faz em hardware o que <code>vhci-hcd</code> faz em software em outros lugares — o dispositivo exportado é enumerado com seu próprio VID/PID, vinculado pelo próprio driver do macOS.<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="firmware & docs esp32-acm"></a>
    </td>
  </tr>
</table>

### NanoKVM com firmware USBridge: 60 FPS em vez de lag

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Vídeo: Sipeed NanoKVM com firmware USBridge transmite a 60 FPS com quase nenhum lag de entrada"></a>
  <br><br>
  <img src="https://img.shields.io/badge/1080p-60_FPS-2da44e?style=flat-square" alt="1080p 60 FPS">
  <img src="https://img.shields.io/badge/Tailscale-P2P-242424?style=flat-square&logo=tailscale&logoColor=white" alt="Tailscale P2P">
  <img src="https://img.shields.io/badge/Audio-sound_%2B_mic-007ec6?style=flat-square" alt="Áudio: som e microfone">
  <img src="https://img.shields.io/badge/FEC-packet_loss_recovery-f59e0b?style=flat-square" alt="FEC: correção de erro para frente">
</div>

Um vídeo de 2 minutos: um Sipeed NanoKVM com grande lag em um laboratório doméstico se torna um KVM a 60 FPS sobre IP com quase nenhum lag de entrada após a instalação do firmware USBridge, rápido o suficiente para jogar com um gamepad. O Tailscale funciona na própria placa, então é acessível de qualquer lugar sem redirecionamento de portas; o som toca em ambas as direções, e o FEC (correção de erro para frente) reconstrói pacotes perdidos, então a imagem permanece limpa no Wi-Fi e pela internet. O firmware para o NanoKVM, Radxa Zero 3W e Radxa Cubie A7Z está na [página do Agente de Hardware](https://www.usbridge.io/hardware-agent/).

## FAQ

<details>
<summary><b>O USBridge funciona com Moonlight e Sunshine?</b></summary>

Sim. O Agente inclui o streamer Sunshine de código aberto, então o cliente Moonlight padrão se conecta a ele. Vibepollo (um fork do Sunshine com Apollo embutido) e punktfunk também estão embutidos e alternam em um clique.
</details>

<details>
<summary><b>Há um streamer Moonlight de hardware?</b></summary>

Sim. Um USBridge KVM 2.0, ou um Radxa Zero 3W, Radxa Cubie A7Z ou Sipeed NanoKVM com firmware USBridge, é um host GameStream compatível com Sunshine para qualquer coisa em sua entrada HDMI: um PC, PS5, Xbox ou Switch. Ele transmite 1080p 60 FPS com áudio para o cliente Moonlight padrão, sem software na fonte. Veja [Streamer Moonlight de Hardware](https://www.usbridge.io/moonlight-hardware-streamer/).
</details>

<details>
<summary><b>Qual streamer é mais rápido no meu PC?</b></summary>

Depende da GPU, do driver e da rede. O benchmark embutido executa Sunshine, RustShine, Vibepollo e punktfunk em seu próprio hardware e rede e os compara. Veja [Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/).
</details>

<details>
<summary><b>Quais GPUs são suportadas para codificação?</b></summary>

NVIDIA NVENC, AMD AMF, Intel QSV e VAAPI no Linux. Máquinas virtuais e PCs sem um codificador GPU usam software H.264 (OpenH264).
</details>

<details>
<summary><b>O que é gratuito e o que precisa de Pro?</b></summary>

Cor da 4:4:4, HDR10, PyroWave, gamepads, microfone, MIDI e passthrough USB bruto são gratuitos. Tablets Wacom com pressão e inclinação, e o firmware KVM de hardware ($19 vitalício por dispositivo, avaliação gratuita de 24h), são Pro.
</details>

<details>
<summary><b>Posso transformar um Sipeed NanoKVM ou uma placa Radxa em um IP-KVM?</b></summary>

Sim. Flash o firmware USBridge da [página do Agente de Hardware](https://www.usbridge.io/hardware-agent/). Você obtém acesso ao BIOS/UEFI, mídia virtual ISO e IMG, instantâneas Btrfs imutáveis, vídeo a 60 FPS, áudio e passthrough USB. Placas Radxa precisam de um dongle de captura HDMI-para-USB.
</details>

<details>
<summary><b>Preciso de redirecionamento de portas?</b></summary>

Não. As conexões vão através do Tailscale peer-to-peer, com travessia NAT automática.
</details>

<details>
<summary><b>Posso compartilhar acesso a um computador com outra pessoa?</b></summary>

Sim. Convide pessoas por e-mail com um papel e uma data de término; os convidados se juntam através do USBridge e não precisam de uma conta Tailscale.
</details>

## Recursos & Licença

<table>
  <tr>
    <td width="33%" valign="top">
      <b>Desenvolvimento & Roteiro</b><br>
      Eu mantenho um painel público para rastrear todos os recursos planejados, atualizações arquitetônicas e cronogramas de lançamento.<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">Ver Roteiro Ao Vivo</a>
    </td>
    <td width="33%" valign="top">
      <b>Links do Projeto</b><br>
      <ul>
        <li><a href="https://usbridge.io">Site Oficial</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (Teste Beta & Bugs)</a></li>
        <li><a href="https://www.reddit.com/r/USBridge/">Reddit</a></li>
        <li><a href="https://www.youtube.com/@KVMUSBridge">YouTube</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Apoio no Patreon</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>Licença (GPLv3)</b><br>
      Este projeto está licenciado sob a <b>GPLv3</b>. O cliente multiplataforma incorpora código da <code>moonlight-common-c</code> (também GPLv3).<br><br>
      <a href="LICENSE">Ver Arquivo de LICENÇA</a>
    </td>
  </tr>
</table>