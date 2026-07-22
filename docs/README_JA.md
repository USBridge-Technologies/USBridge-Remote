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

**USBridge Remote** は、リモートマシンを管理するための統合された高性能クライアントです。これは、**ハードウェアレベルのBIOSアクセス**（USBridge KVMデバイス経由）と**ソフトウェアベースのリモートデスクトップ**を単一の洗練されたインターフェースに統合するように設計されています。

 🖥️ **OSが起動する前にハードウェアレベルのBIOS制御が必要ですか？**  
 USBridge Remoteは、**USBridge-KVM 2.0**とネイティブに統合されており、アウトオブバンドのメタルレベル管理を提供します。

[![Crowd Supply KVM 2.0](https://img.shields.io/badge/Crowd_Supply-USBridge--KVM_2.0-2da44e?style=for-the-badge&logo=crowdsupply&logoColor=white)](https://www.crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)


> ⚠️ **ベータソフトウェア** — これは初期リリースです。バグが予想されます。問題を報告するには、[GitHub Issues](https://github.com/USBridge-Technologies/USBridge-Remote/issues)を通じて、またはサポートのために私たちの[Discord](https://discord.com/invite/xqQ6ybkfWS)に参加してください。
> 
> ℹ️ **Windows Defender / Antivirusの誤検知に関する注意:**  
> Windows Defenderは、`libva.dll`を脅威（`Trojan:Win32/Wacatac.B!ml`）として誤ってフラグ付けする可能性があります。これは、署名されていないバイナリに対するヒューリスティック/機械学習検出によるものです。**これは誤検知です。**  
> 私たちは、このファイルをMicrosoft Security Intelligenceに公式レビューとホワイトリストのために提出しました。その間に、もしあなたのアンチウイルスが`libva.dll`を削除した場合は、隔離から復元するか、USBridgeフォルダーをアンチウイルスの除外リストに追加してください。

---

## ダウンロード

### クライアント
クライアントは制御インターフェースであり、ワークステーションまたはラップトップにインストールされます。接続、ライブリモートデスクトップ、仮想デバイスのパススルー、スナップショットレジストリを管理します。

| アーキテクチャ | Windows | macOS | Linux | Android | iOS |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **x86_64** | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Windows-x86_64.zip) | — | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Linux-x86_64.AppImage) | — | — |
| **ARM64** | — | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-macOS-arm64.dmg) | — | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64.apk) | [App Store](https://apps.apple.com/us/app/usbridge-client/id6787665935) |

## エージェント

エージェントは、リモートでアクセスしたいターゲットマシン（サーバーまたはPC）で実行されます。画面キャプチャ、入力注入、Tailscaleネットワーキングを処理します。

| アーキテクチャ | Windows | macOS | Linux |
| :--- | :---: | :---: | :---: |
| **x86_64** | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Windows-x86_64.zip) | — | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-Linux-x86_64.AppImage) |
| **ARM64** | — | [ダウンロード](https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeAgent-macOS-arm64.dmg) | — |

---

## デモ

<div align="center">
  <a href="https://youtu.be/1pV9PJeBr7M">
    <img src="https://img.youtube.com/vi/1pV9PJeBr7M/maxresdefault.jpg" alt="USBridge Remote Demo" style="max-width: 100%; border-radius: 8px;">
  </a>
</div>

---

## メディアでの紹介

| メディア | ハイライト | リンク |
| :--- | :--- | :---: |
| **IlSoftware.it** (イタリア) | *"USBridge RemoteはRustDeskとAnyDeskに挑戦..."* — ソフトウェアエージェントとハードウェアKVMの相乗効果、ネイティブのWaylandサポート、P2Pアーキテクチャを称賛する独立した詳細レビュー。 | [記事を読む](https://www.ilsoftware.it/alternativa-rustdesk-anydesk-usbridge/) |

---

## 機能

<img width="2000" height="1046" alt="USBridge_ap4p" src="https://github.com/user-attachments/assets/2b4bfdf8-412f-4cd7-b4c4-3794d72475cc" />

**すべてを一つの場所に** — ワークフローを統一しました。USBridge KVMハードウェアとソフトウェアエージェントを単一のダッシュボードから管理します。マシンを追加し、接続すれば、すぐに利用可能です。

**制限なし、サブスクリプションなし** — 完全無料。セッション時間の制限なし、接続の上限なし、ターゲットマシンでのアカウントは不要です。

**低遅延ビデオ & Moonlight統合** — 滑らかな240 FPSで最大2K解像度を楽しめます。私の適応ストリーミングエンジンは、ネイティブのMoonlight統合を活用して、比類のない超低遅延のリモートデスクトップパフォーマンスを提供します。

**Tailscale統合** — 組み込みの暗号化P2Pトンネリング。ポートフォワーディングやファイアウォールルールをいじることなく、世界中の任意のマシンに接続できます。LANおよびインターネット上で自動的に機能します。

**共有クリップボード** — ローカルマシンとリモートターゲット間でシームレスにコピー＆ペーストが可能です。テキスト、画像、ファイル転送を完全にサポートしています。

<img width="2080" height="1170" alt="Screenshot 2026-05-03 20112н0" src="https://github.com/user-attachments/assets/06dc3de0-2be9-42f7-a897-830a0a6f2bc7" />


---

## Waylandサポート（プロンプトなし）

Linux上のほとんどのリモートデスクトップエージェントはWaylandで苦労しており、セッションが開始されるたびに許可プロンプトや確認ポップアップでスパムされます。

私はUSBridgeエージェントをWaylandをネイティブにサポートするように設計しました。フルスクリーンキャプチャと入力注入を**面倒な許可プロンプトや手動確認なしで**処理します。単に動作します。

---

## クイックスタート

1. **エージェントをインストール**します。リモートでアクセスしたいマシンにインストールしてください。起動すると、接続トークンとTailscaleアドレスが表示されます。インターネット経由でアクセスする必要がある場合は、Tailscaleに接続してください。

2. **クライアントをインストール**します。ワークステーション、ラップトップ、または電話にインストールしてください。

3. **接続を追加**します — エージェントウィンドウに表示されているIPまたはTailscaleアドレスを入力します。それだけです。

---

## プロジェクトロードマップ

私はソフトウェア開発計画と今後の機能をオープンダッシュボードで管理しています。現在開発中のもの、計画されているもの、今後の機能のステータスを確認したい場合は、ライブロードマップをチェックしてください：

 **[USBridge Remoteロードマップを見る](https://github.com/orgs/USBridge-Technologies/projects/3)**

---

## コミュニティ & ベータテスト

私たちのDiscordに参加して、**ベータテスター**の役割を取得し、バグを報告し、ロードマップの形成を手伝ってください：

**[discord.com/invite/xqQ6ybkfWS](https://discord.com/invite/xqQ6ybkfWS)**

---

## リンク

- 🌐 [公式ウェブサイト](https://usbridge.io)
- ❤️ [Patreonページ](https://www.patreon.com/USBridge_Technologies)
- 🛒 [Crowd SupplyのUSBridge KVM 2.0](https://crowdsupply.com/usbridge-technologies/usbridge-kvm-2-0)
- 💬 [Discord](https://discord.com/invite/xqQ6ybkfWS)