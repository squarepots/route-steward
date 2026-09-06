# Route Steward

[English](README.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md) · [Español](README.es.md) · [Português (Brasil)](README.pt-BR.md)

[Quickstart](docs/QUICKSTART.md) · [FAQ](docs/FAQ.md) · [Compatibility](docs/COMPATIBILITY.md) · [Security](SECURITY.md) · [Releases](https://github.com/squarepots/route-steward/releases)

**AI エージェントを使って、自分で管理するサーバー上のプライベートプロキシ経路を構築・保守します。**

利用するサーバー、経路の使い方、クライアントをエージェントに伝えると、Route Steward が対応するプロキシ経路を展開し、実トラフィックを確認し、Clash Verge や Shadowrocket などの対応クライアント向け設定を生成します。

## AI エージェントに渡す

```text
Open https://github.com/squarepots/route-steward and use its Route Steward skill to set up or manage a private proxy on servers I control.
```

導入と初回利用は [Quickstart](docs/QUICKSTART.md) を参照してください。

## 主な機能

- Hysteria2 の直接経路と 2 台構成の WireGuard リレー
- 経路のヘルス確認、ドリフト確認、再開可能なサーバー交換
- 対応するデスクトップ、モバイル、ヘッドレスクライアント向けのプライベート設定
- Mihomo/Clash Verge 互換クライアントと Shadowrocket 向けの任意のプライベート購読配信

現在対応しているホスト、クライアント、プロトコル、配信方法は [Compatibility](docs/COMPATIBILITY.md) にあります。ホスト変更、移行、復旧の詳細は [Operations](OPERATIONS.md) を参照してください。

秘密情報と生成済み設定は指定した private directory に保存されます。クラウド AI は操作に必要な入力を処理する場合があります。[Privacy](docs/PRIVACY.md) と [Security](SECURITY.md) を参照してください。

Route Steward は [AGPL-3.0-only](LICENSE) です。
