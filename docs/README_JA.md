# USBridge Remote

<div align="center">
  <img src="./assets/banner6.svg" width="1400" alt="USBridge Remote: low-latency remote desktop and KVM-over-IP">
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

**USBridge Remote** は低遅延の **リモートデスクトップ** と **KVM-over-IP** エコシステムです：Windows、macOS、Linux、Android、iOS、ブラウザ用のクライアント、内蔵の **Sunshine / Moonlight** ゲームストリーマーを備えたホストエージェント、**Radxa Zero 3W/3E**、**Radxa Cubie A7Z**、**Sipeed NanoKVM** ボード用のIP-KVMファームウェアが含まれています。これは、**ハードウェアレベルのBIOSアクセス** と **ソフトウェアリモートデスクトップ** を単一のインターフェースに統合し、ゲームパッド、Wacomタブレット、MIDI、マイク用のUSBパススルー、ポートフォワーディングなしのTailscale P2Pネットワーキングを提供します。

<div align="center">
  <img src="./assets/Functions.svg" width="1400" alt="USBridge Remote features: remote desktop, game streaming, USB passthrough, hardware KVM">
</div>


## ダウンロード

**オールインワンインストーラー (クライアント + エージェント、セットアップ中にいずれかまたは両方を選択)：** [Windowsセットアップ (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) · [macOSディスクイメージ (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-macOS-arm64.dmg)

すべてのファイルは [最新リリース](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest) にあります。

### クライアント
クライアントは制御インターフェースです — ワークステーションまたはラップトップにインストールされるか（またはブラウザで直接実行されます）。接続、ライブリモートデスクトップ、仮想デバイスパススルー、スナップショットレジストリを管理します。

| プラットフォーム | インストーラー | ポータブル | パッケージ / ストア |
| :--- | :--- | :--- | :--- |
| **Windows** x86_64 | [セットアップ (.exe)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridge-Windows-x86_64-Setup.exe) | [ポータブル (.zip)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — |
| **macOS** Apple Silicon | [ディスクイメージ (.dmg)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | — |
| **Linux** x86_64 | [Debian / Ubuntu (.deb)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.deb) · [Fedora / RHEL (.rpm)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.rpm) · [Arch (.pkg.tar.zst)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.pkg.tar.zst) | [AppImage](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | [APT · DNF · pacmanリポジトリ](#install-from-the-terminal) |
| **Android** ARM64 | [APK (自己更新)](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-selfupdate.apk) | — | [Google Play](https://play.google.com/store/apps/details?id=io.usbridge.client) |
| **iOS / iPadOS** | — | — | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |
| **ウェブブラウザ** | — | [web.usbridge.ioを開く](https://web.usbridge.io) | — |

### ターミナルからインストール

1つのコマンドでクライアントとエージェントをインストールします。Linuxでは、ディストリビューションに適したネイティブパッケージを選択します（Debian/Ubuntuではapt、Fedora/RHELではdnf、Archではpacman）、そしてパッケージは署名されたUSBridgeリポジトリを追加するので、アプリはシステムの残りの部分と一緒に更新されます：

```sh
# LinuxとmacOS
curl -fsSL https://www.usbridge.io/install.sh | sh
# エージェントのみ（リモートでのみアクセスするマシン）
curl -fsSL https://www.usbridge.io/install.sh | sh -s -- --agent
```

```powershell
# Windows (PowerShell)
irm https://www.usbridge.io/install.ps1 | iex
```

<details>
<summary><b>APTリポジトリを手動で追加する（Debian / Ubuntu）</b></summary>

```sh
curl -fsSL https://raw.githubusercontent.com/USBridge-Technologies/USBridge-Remote/main/scripts/deb/usbridge-archive-keyring.gpg \
  | sudo tee /usr/share/keyrings/usbridge-archive-keyring.gpg >/dev/null
echo "deb [arch=amd64 signed-by=/usr/share/keyrings/usbridge-archive-keyring.gpg] https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download ./" \
  | sudo tee /etc/apt/sources.list.d/usbridge.list
sudo apt update && sudo apt install usbridge-client usbridge-agent
```

ダウンロードした `.deb`、`.rpm` または Arch パッケージは、インストール時に同じリポジトリを設定します。
</details>

🌐 **ゼロインストールウェブクライアント**：インストールは不要です。接続するには [web.usbridge.io](https://web.usbridge.io) を開くだけです。 *(注：ウェブクライアントは、ブラウザのセキュリティサンドボックスとWebRTCの制約により、一部の機能とパフォーマンスに制限があります。完全な体験を得るには、ネイティブアプリを使用してください。)* ウェブクライアントは、信頼された `<label>.device.usbridge.io` 名を介してエージェントに到達します：エージェントは、ブラウザが初めてアクセスしたときにその証明書を取得し、あなたがエージェントにサインインしている間のみ有効です — そのため、最初の試行には数分かかることがあります。エージェントの **ステータス → 証明書** 行、または [エージェントドキュメント](agent/docs/README.md#platform-notes-from-the-top-level-readme) で詳細を確認してください。USBridge KVM は、`https://<kvm>:9443/` でウェブクライアント自体にもサービスを提供し、可能な場合は独自の信頼された名前に移行します。

## エージェント

エージェントは、リモートでアクセスしたいターゲットマシン — サーバーまたはPCで実行されます。画面と音声をキャプチャし、キーボード、マウス、ゲームパッドをそのマシンにパススルーし、Tailscaleを介して接続します。

<table>
  <tr>
    <!-- 左列: ダウンロードテーブル -->
    <td valign="middle">
      <table>
        <tr>
          <th>プラットフォーム</th>
          <th>ダウンロード</th>
        </tr>
        <tr>
          <td><b>Windows</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64-Setup.exe">セットアップ (.exe)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip">ポータブル (.zip)</a></td>
        </tr>
        <tr>
          <td><b>macOS</b><br>Apple Silicon</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg">ディスクイメージ (.dmg)</a></td>
        </tr>
        <tr>
          <td><b>Linux</b><br>x86_64</td>
          <td><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.deb">Debian / Ubuntu (.deb)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.rpm">Fedora / RHEL (.rpm)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.pkg.tar.zst">Arch (.pkg.tar.zst)</a><br><a href="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage">AppImage (ポータブル)</a></td>
        </tr>
      </table>
    </td>
    <!-- 右列: 画像 -->
    <td valign="middle" width="450">
      <img src="./assets/agent-screenshot.svg" alt="USBridge Agent window: connection token, Tailscale address and stream status" width="100%">
    </td>
  </tr>
</table>

## 機能

**ハードウェアアクセラレーションエンコーディング：** NVIDIA **NVENC** · AMD **AMF** · Intel **QSV** · Linux **VAAPI** · さらに、GPUエンコーダーのないVMやマシン用のソフトウェア **H.264 (OpenH264)**

<table>
  <tr>
    <td width="33%" valign="top">
      <code>基本機能</code><br><br>
      <b>SUNSHINE オープンソース</b><br>
      <i>標準ゲームストリーミング</i><br><br>
      ✓ 内蔵のオープンソースSunshineストリーマー<br>
      ✓ <a href="https://www.usbridge.io/sunshine-vs-apollo/">代替ストリーマー</a>: Vibepollo (Apolloが組み込まれたSunshineフォーク、Windows) と punktfunk (Moonlight互換、Windows & Linux)、1クリックで切り替え<br>
      ✓ 自分のハードウェアとネットワークでSunshine、RustShine、Vibepollo、punktfunkを比較するための内蔵ベンチマーク<br>
      ✓ 低遅延のデスクトップ＆ゲームアクセス<br>
      ✓ 双方向ファイル＆テキストクリップボード<br>    
      ✓ マルチモニタ表示切り替え<br>
      ✓ 統合されたTailscale P2Pネットワーキング<br>
      ✓ ゲストアクセス：役割と終了日を指定してメールで人を招待<br>
      ✓ ネイティブWaylandサポート（プロンプトなしのキャプチャ）
    </td>
    <td width="33%" valign="top">
      <code>+ 基本機能</code><br><br>
      <b>USBRIDGE FREE</b><br>
      <i>Rustベースのストリームエンジン</i><br><br>
      ✓ 即時接続カスタムリモートプロトコル<br>
      ✓ 生の <a href="https://www.usbridge.io/usb-passthrough/">USB周辺機器パススルー</a><br>
      ✓ Wi-Fiの安定性に最適化された適応ストリーミング<br>
      ✓ HDR10<br>
      ✓ ロスレス4:4:4クロマカラー精度<br>
      ✓ マイクパススルー（クライアント → ホスト）<br>
      ✓ MIDIデバイスパススルー（クライアント → ホスト）<br>
      ✓ GPUエンコーディング：NVENC、AMF、QSV、VAAPI<br>
      ✓ GPUのないVM用のソフトウェアH.264 (OpenH264)<br>
      ✓ <a href="https://www.usbridge.io/pyrowave/">PyroWave</a> 低遅延ウェーブレットコーデック（Valve製）<br>
      ✓ ヘッドレス仮想ディスプレイ管理<br>
      ✓ 低遅延ゲームパッドコントローラーパススルー<br>
      ✓ ウェブブラウザクライアントアクセス（インストール不要）<br>
      ✓ Windowsのログイン前アクセス（安全な資格情報入力）
    </td>
    <td width="33%" valign="top">
      <code>+ 基本機能</code> <code>+ 無料機能</code><br><br>
      <b>USBRIDGE PRO</b><br>
      <i>プロフェッショナルワークフロー</i><br><br>
      ✓ 圧力と傾きに対応したWacomタブレットサポート（<a href="https://www.usbridge.io/macos-wacom-support/">macOSでも</a>）<br>
      ✓ <a href="https://www.usbridge.io/hardware-agent/">ハードウェアKVMファームウェア</a> Radxa Zero 3W/3E、Radxa Cubie A7Z、Sipeed NanoKVM用：BIOS/UEFIアクセス、ISO仮想メディア、Btrfsスナップショット（デバイスごとに$19の生涯ライセンス、24時間の無料評価）<br>
      ✓ <a href="https://www.usbridge.io/moonlight-hardware-streamer/">ハードウェアMoonlightストリーマー</a> これらのボード上：任意のHDMIソース（PC、PS5、Xbox、Switch）をMoonlightに1080p 60 FPSで、ソースにソフトウェアは不要<br><br>
    </td>
  </tr>
</table>

## クイックスタート

1. **エージェントをインストール** したいマシンにインストールします。起動すると、接続トークンとTailscaleアドレスが表示されます。インターネット経由でアクセスする必要がある場合は、Tailscaleを接続してください。

2. **クライアントをインストール** ワークステーション、ラップトップ、または電話にインストールします。

3. **接続を追加** — エージェントウィンドウに表示されているIPまたはTailscaleアドレスを入力します。それだけです。

<div align="center">
  <img src="./assets/QuickStart.svg" width="1400" alt="USBridge Remote quick start: install the agent, install the client, connect">
</div>

## ハードウェア統合

<table>
  <tr>
   <td width="33%" valign="top">
      <b>ハードウェアレベルのBIOS制御</b><br>
      USBridge Remoteは、OSブート前のアウトオブバンド、ベアメタル管理のためにUSBridge-KVM 2.0アプライアンスとネイティブに統合されています。また、初のハードウェアMoonlightストリーマーでもあります：標準のMoonlightクライアントはKVMとペアリングし、HDMI入力の任意のソース（PCまたはゲームコンソール）をストリーミングします。<br><br>
      <a href="https://www.usbridge.io/hardware-agent?utm_campaign=readme-kvm&utm_medium=readme&utm_source=github#buy-usbridge-kvm-2-0"><img src="https://img.shields.io/badge/Buy-USBridge--KVM_2.0-2da44e?style=for-the-badge" alt="Buy USBridge-KVM 2.0"></a>
    </td>
    <td width="33%" valign="top">
      <b>DIY IP-KVMファームウェア</b><br>
      HDMI-to-USBキャプチャドングルを使用して、互換性のあるボード（Radxa Zero 3W/3E、Radxa Cubie A7Z）に公式ファームウェアをフラッシュして、自分だけのIP-KVMを構築します。<b>Sipeed NanoKVM</b>では、標準のファームウェアを置き換えます：カクカクした映像の代わりにスムーズな60 FPS、音声とマイク、ゲームパッド、USBパススルー、Moonlightサポートを提供します（<a href="https://www.usbridge.io/hardware-agent/">ファームウェアをダウンロード</a>）。<br><br>
      <a href="https://www.usbridge.io/hardware-agent"><img src="https://img.shields.io/badge/DOWNLOAD-DIY_FIRMWARE-007ec6?style=for-the-badge" alt="Get the Firmware"></a>
    </td>
    <td width="33%" valign="top">
      <b>USB/IPハードウェアドングル (ESP32-S3)</b><br>
      ソフトウェアUSBパススルーはmacOSで壁にぶつかります：接続するVHCIドライバーがないため、リモートWacomタブレット（または他のUSBデバイス）を実際のハードウェアとして提示できません。このドングルはMacに接続し、他の場所でソフトウェアが行うことをハードウェアで実行します — エクスポートされたデバイスは独自のVID/PIDで列挙され、macOSの独自のドライバーによってバインドされます。<br><br>
      <a href="esp32-acm/README.md"><img src="https://img.shields.io/badge/Open_Hardware-esp32--acm-f59e0b?style=for-the-badge" alt="esp32-acm firmware & docs"></a>
    </td>
  </tr>
</table>

### USBridgeファームウェアを搭載したNanoKVM：60 FPSの遅延なし

<div align="center">
  <a href="https://www.youtube.com/watch?v=q8rX7o-Svqs"><img src="https://img.youtube.com/vi/q8rX7o-Svqs/maxresdefault.jpg" width="720" alt="Video: Sipeed NanoKVM flashed with USBridge firmware streams at 60 FPS with almost no input lag"></a>
  <br><br>
  <img src="https://img.shields.io/badge/1080p-60_FPS-2da44e?style=flat-square" alt="1080p 60 FPS">
  <img src="https://img.shields.io/badge/Tailscale-P2P-242424?style=flat-square&logo=tailscale&logoColor=white" alt="Tailscale P2P">
  <img src="https://img.shields.io/badge/Audio-sound_%2B_mic-007ec6?style=flat-square" alt="Audio: sound and microphone">
  <img src="https://img.shields.io/badge/FEC-packet_loss_recovery-f59e0b?style=flat-square" alt="FEC: forward error correction">
</div>

2分間のビデオ：自宅のラボで重度の遅延があるSipeed NanoKVMが、USBridgeファームウェアをフラッシュした後、ほぼ遅延なしで60 FPSのKVM over IPになります。ゲームパッドでゲームをプレイするのに十分な速さです。Tailscaleはボード自体で動作するため、ポートフォワーディングなしでどこからでもアクセス可能です。音声は双方向で再生され、FEC（前方誤り訂正）が失われたパケットを再構築するため、Wi-Fiやインターネット上で映像がクリアに保たれます。NanoKVM、Radxa Zero 3W、Radxa Cubie A7Z用のファームウェアは、[ハードウェアエージェントページ](https://www.usbridge.io/hardware-agent/)にあります。

## FAQ

<details>
<summary><b>USBridgeはMoonlightとSunshineと互換性がありますか？</b></summary>

はい。エージェントはオープンソースのSunshineストリーマーを搭載しているため、標準のMoonlightクライアントが接続します。Vibepollo（Apolloが組み込まれたSunshineフォーク）とpunktfunkも内蔵されており、1クリックで切り替えが可能です。
</details>

<details>
<summary><b>ハードウェアMoonlightストリーマーはありますか？</b></summary>

はい。USBridge KVM 2.0、またはUSBridgeファームウェアをフラッシュしたRadxa Zero 3W/3E、Radxa Cubie A7Z、Sipeed NanoKVMは、HDMI入力に接続されたPC、PS5、Xbox、SwitchなどのSunshine互換GameStreamホストです。ソースにソフトウェアは不要で、標準のMoonlightクライアントに1080p 60 FPSで音声をストリーミングします。詳細は[ハードウェアMoonlightストリーマー](https://www.usbridge.io/moonlight-hardware-streamer/)を参照してください。
</details>

<details>
<summary><b>私のPCで最も速いストリーマーはどれですか？</b></summary>

GPU、ドライバー、ネットワークによります。内蔵ベンチマークは、Sunshine、RustShine、Vibepollo、punktfunkを自分のハードウェアとネットワークで実行し、比較します。詳細は[Sunshine vs Apollo vs Vibepollo vs Punktfunk](https://www.usbridge.io/sunshine-vs-apollo/)を参照してください。
</details>

<details>
<summary><b>エンコーディングに対応しているGPUはどれですか？</b></summary>

NVIDIA NVENC、AMD AMF、Intel QSV、LinuxのVAAPIです。GPUエンコーダーのない仮想マシンやPCは、ソフトウェアH.264 (OpenH264)を使用します。
</details>

<details>
<summary><b>無料の機能とProが必要な機能は何ですか？</b></summary>

4:4:4カラー、HDR10、PyroWave、ゲームパッド、マイク、MIDI、そして生のUSBパススルーは無料です。圧力と傾きに対応したWacomタブレットとハードウェアKVMファームウェア（デバイスごとに$19の生涯ライセンス、24時間の無料評価）はProです。
</details>

<details>
<summary><b>Sipeed NanoKVMまたはRadxaボードをIP-KVMに変えることはできますか？</b></summary>

はい。[ハードウェアエージェントページ](https://www.usbridge.io/hardware-agent/)からUSBridgeファームウェアをフラッシュしてください。BIOS/UEFIアクセス、ISOおよびIMG仮想メディア、不変のBtrfsスナップショット、60 FPSのビデオ、音声、USBパススルーが得られます。RadxaボードにはHDMI-to-USBキャプチャドングルが必要です。
</details>

<details>
<summary><b>ポートフォワーディングは必要ですか？</b></summary>

いいえ。接続はTailscaleピアツーピア経由で行われ、自動NATトラバーサルが行われます。
</details>

<details>
<summary><b>他の人とコンピュータへのアクセスを共有できますか？</b></summary>

はい。役割と終了日を指定してメールで人を招待できます。ゲストはUSBridgeを介して参加し、Tailscaleアカウントは必要ありません。
</details>

## リソースとライセンス

<table>
  <tr>
    <td width="33%" valign="top">
      <b>開発とロードマップ</b><br>
      計画されたすべての機能、アーキテクチャの更新、リリーススケジュールを追跡するための公開ダッシュボードを維持しています。<br><br>
      <a href="https://github.com/orgs/USBridge-Technologies/projects/3">ライブロードマップを見る</a>
    </td>
    <td width="33%" valign="top">
      <b>プロジェクトリンク</b><br>
      <ul>
        <li><a href="https://usbridge.io">公式ウェブサイト</a></li>
        <li><a href="https://discord.com/invite/xqQ6ybkfWS">Discord (ベータテスト & バグ)</a></li>
        <li><a href="https://www.reddit.com/r/USBridge/">Reddit</a></li>
        <li><a href="https://www.youtube.com/@KVMUSBridge">YouTube</a></li>
        <li><a href="https://www.patreon.com/USBridge_Technologies">Patreonサポート</a></li>
      </ul>
    </td>
    <td width="33%" valign="top">
      <b>ライセンス (GPLv3)</b><br>
      このプロジェクトは<b>GPLv3</b>の下でライセンスされています。マルチプラットフォームクライアントは、<code>moonlight-common-c</code>（これもGPLv3）からのコードベースを組み込んでいます。<br><br>
      <a href="LICENSE">LICENSEファイルを見る</a>
    </td>
  </tr>
</table>