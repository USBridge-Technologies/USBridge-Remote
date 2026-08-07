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

**USBridge Remote** é um cliente unificado de alto desempenho para gerenciar máquinas remotas. Eu o projetei para combinar **acesso ao BIOS em nível de hardware** (via dispositivos USBridge KVM) e **desktop remoto baseado em software** em uma única interface simplificada.

 🖥️ **Precisa de controle do BIOS em nível de hardware antes do sistema operacional iniciar?**  
 USBridge Remote integra-se nativamente com **USBridge-KVM 2.0** para gerenciamento fora de banda, em nível de metal.

[![Crowd Supply KVM 2.0](https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?style=for-the-badge&logo=crowdsupply&logoColor=white)](https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)


> ⚠️ **Software Beta** — Esta é uma versão inicial. Espere bugs. Por favor, relate problemas via [GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues) ou junte-se ao nosso [Discord](https://discord.com/invite/xqQ6ybkfWS) para suporte.
> 
> ℹ️ **Nota sobre Falsos Positivos do Windows Defender / Antivirus:**  
> O Windows Defender pode sinalizar incorretamente `libva.dll` como uma ameaça (`Trojan:Win32/Wacatac.B!ml`) devido à detecção heurística/aprendizado de máquina em binários não assinados. **Isso é um falso positivo.**  
> Nós enviamos o arquivo para a Microsoft Security Intelligence para revisão oficial e inclusão na lista branca. Enquanto isso, se o seu antivírus remover `libva.dll`, por favor, restaure-o da Quarentena ou adicione a pasta USBridge à sua lista de exclusão do antivírus.

---

## Download

### Cliente
O Cliente é a interface de controle — instalada na sua estação de trabalho ou laptop. Ele gerencia conexões, desktop remoto ao vivo, passagem de dispositivos virtuais e registro de snapshots.

| Arquitetura | Windows | macOS | Linux | Android | iOS |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-selfupdate.apk) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |

## Agente

O Agente é executado na máquina alvo — o servidor ou PC que você deseja acessar remotamente. Ele lida com captura de tela, injeção de entrada e rede Tailscale.

| Arquitetura | Windows | macOS | Linux |
| :--- | :---: | :---: | :---: |
| **x86_64** | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip) | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage) |
| **ARM64** | — | [Download](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg) | — |

---

## Demonstração

<div align="center">
  <a href="https://youtu.be/1pV9PJeBr7M">
    <img src="https://img.youtube.com/vi/1pV9PJeBr7M/maxresdefault.jpg" alt="Demonstração do USBridge Remote" style="max-width: 100%; border-radius: 8px;">
  </a>
</div>

---

## Na Mídia

| Mídia | Destaque | Link |
| :--- | :--- | :---: |
| **IlSoftware.it** (Itália) | *"USBridge Remote desafia RustDesk e AnyDesk..."* — Revisão independente aprofundada elogiando a sinergia do agente de software & hardware KVM, suporte nativo ao Wayland e arquitetura P2P. | [Leia o Artigo](https://www.ilsoftware.it/alternativa-rustdesk-anydesk-usbridge/) |

---

## Recursos

<img width="2000" height="1046" alt="USBridge_ap4p" src="https://github.com/user-attachments/assets/2b4bfdf8-412f-4cd7-b4c4-3794d72475cc" />

**Um lugar para tudo** — Eu unifiquei o fluxo de trabalho. Gerencie o hardware USBridge KVM e os agentes de software a partir de um único painel. Adicione uma máquina, conecte-se e você está dentro.

**Sem limites, sem assinaturas** — Completamente gratuito. Sem limites de tempo de sessão, sem limites de conexão e sem conta necessária na máquina alvo.

**Vídeo de baixa latência & Integração com Moonlight** — Aproveite até 2K de resolução com 240 FPS suaves como manteiga e zero latência perceptível. Meu mecanismo de streaming adaptativo aproveita a integração nativa com o Moonlight para oferecer um desempenho de desktop remoto incomparável e ultra-baixa latência.

**Integração com Tailscale** — Tunelamento P2P criptografado embutido. Conecte-se a qualquer máquina globalmente sem se preocupar com encaminhamento de portas ou regras de firewall. Funciona em LAN e pela internet automaticamente.

**Área de Transferência Compartilhada** — Copie e cole perfeitamente entre sua máquina local e alvos remotos. Suporta totalmente texto, imagens e transferências de arquivos de forma nativa.

**Suporte a Múltiplos Monitores** — Eu adicionei a capacidade de alternar entre várias telas. Se a máquina alvo tiver vários monitores, agora você pode facilmente selecionar qual deseja visualizar diretamente nas configurações de conexão. 

<img width="2080" height="1170" alt="Screenshot 2026-05-03 20112н0" src="https://github.com/user-attachments/assets/06dc3de0-2be9-42f7-a897-830a0a6f2bc7" />


---

## Suporte ao Wayland (Sem Solicitações)

A maioria dos agentes de desktop remoto no Linux tem dificuldades com o Wayland ou constantemente o incomodam com solicitações de permissão e pop-ups de confirmação toda vez que uma sessão começa. 

Eu projetei o Agente USBridge para suportar o Wayland nativamente. Ele lida com captura de tela completa e injeção de entrada de forma nativa **sem solicitações de permissão irritantes** ou confirmações manuais. Funciona perfeitamente.

---

## Início Rápido

1. **Instale o Agente** na máquina que você deseja acessar remotamente. Inicie-o — ele exibirá um token de conexão e um endereço Tailscale. Conecte-se ao Tailscale se precisar de acesso pela internet.

2. **Instale o Cliente** na sua estação de trabalho, laptop ou telefone.

3. **Adicione uma conexão** — insira o IP ou endereço Tailscale mostrado na janela do Agente. É isso.

---

##  Roteiro do Projeto

Eu gerencio os planos de desenvolvimento de software e os recursos futuros em um painel aberto. Se você quiser ver o que está sendo desenvolvido atualmente, o que está planejado ou acompanhar o status dos recursos futuros, confira o roteiro ao vivo:

 **[Ver Roteiro do USBridge Remote](https://github.com/orgs/USBridge-Technologies/projects/3)**

---

## Comunidade & Testes Beta

Junte-se ao nosso Discord para obter o papel de **Testador Beta**, relatar bugs e me ajudar a moldar o roteiro:

**[discord.com/invite/xqQ6ybkfWS](https://discord.com/invite/xqQ6ybkfWS)**

---

## Links

- 🌐 [Site Oficial](https://usbridge.io)
- ❤️ [Página do Patreon](https://www.patreon.com/USBridge_Technologies)
- 🛒 [USBridge KVM 2.0 no Crowd Supply](https://crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)
- 💬 [Discord](https://discord.com/invite/xqQ6ybkfWS)