# Mister Morph

ローカルまたは各種チャネル接続で動かす Agent 向けのデスクトップ App、CLI、そして再利用可能な Go ランタイムです。

他の言語：[English](../../README.md) | [简体中文](../zh-CN/README.md)

まず触ってみたいだけなら、いまはデスクトップ App が最短です。Console UI を同梱し、ローカルバックエンドの起動も引き受け、初回セットアップも App 内で完結します。

## Mister Morph を選ぶ理由

- 🖥️ App 起点で始めやすい: デスクトップ App によって以前のような複数ターミナル前提の導入が不要になり、必要なら CLI もそのまま使えます。
- 🧩 再利用可能な Go コア: デスクトップ App、CLI、Console バックエンドとして動かすだけでなく、他の Go プロジェクトへ組み込むこともできます。
- 🤝 Connection: [Aqua](https://mistermorph.com/aqua) により、複数の agent が会話し、いっしょに計画し、協調できます。
- 🛠️ 実用的な拡張モデル: 組み込みツール、`SKILL.md` ベースのスキル、Go への組み込みでローカル利用から自動化、統合までカバーします。
- 🔒 セキュリティを前提にした設計: auth profiles、送信先ポリシー、承認、秘匿化がランタイムモデルに組み込まれています。

## クイックスタート

### デスクトップ App（推奨）

1. [GitHub Releases](https://github.com/quailyquaily/mistermorph/releases) ページから対象プラットフォームの配布物を取得します。
   - macOS: `mistermorph-desktop-darwin-arm64.dmg`
   - Linux: `mistermorph-desktop-linux-amd64.AppImage` または `mistermorph-desktop-linux-amd64.deb`
   - Windows: `mistermorph-desktop-windows-amd64.zip`
2. App を起動します。
3. App 内のセットアップフローを完了します。
4. そのまま Console UI を使います。`morph console serve` を手動で起動する必要はありません。

ビルド、パッケージ、プラットフォーム別メモは [../app.md](../app.md) を参照してください。

### CLI

まず CLI をインストールします。

```bash
curl -fsSL -o /tmp/install-mistermorph.sh https://raw.githubusercontent.com/quailyquaily/mistermorph/refs/heads/master/scripts/install-release.sh
sudo bash /tmp/install-mistermorph.sh
```

またはソースからインストールします。

```bash
git clone https://github.com/quailyquaily/mistermorph.git
cd mistermorph
go build -o "$(go env GOPATH)/bin/morph" ./cmd/mistermorph
```

ワークスペースを初期化し、API Key を設定して、1 回タスクを実行します。

```bash
morph install
export MISTER_MORPH_LLM_API_KEY="YOUR_API_KEY"
morph run --task "Hello!"
```

まだ `config.yaml` がない場合、`morph install` がセットアップウィザードを起動し、必要なワークスペースファイルを書き出します。

CLI モードと設定の詳細は [../modes.md](../modes.md) と [../configuration.md](../configuration.md) を参照してください。

## Mister Morph に含まれるもの

- 初回セットアップと内蔵 Console UI を備えたデスクトップ App
- 単発タスク、スクリプト、自動化、サーバーモード向けの CLI
- ブラウザ上で設定、運用、監視を行うローカル Console サーバー
- Telegram、Slack、LINE、Lark 向けランタイム
- 他プロジェクトへ組み込める Go 統合レイヤー
- 組み込みツール群と `SKILL.md` ベースのスキルシステム
- auth profiles、送信先ポリシー、承認、秘匿化などのセキュリティ制御

## ドキュメント

まず読むもの：

- [デスクトップ App](../app.md)
- [モード](../modes.md)
- [設定](../configuration.md)
- [トラブルシュート](../troubleshoots.md)

リファレンス：

- [Console](../console.md)
- [Aqua 通信](../aqua.md)
- [Tools](../tools.md)
- [Skills](../skills.md)
- [Security](../security.md)
- [Integration](../integration.md)
- [Architecture](../arch.md)

チャネル設定：

- [Telegram](../telegram.md)
- [Slack](../slack.md)
- [LINE](../line.md)
- [Lark](../lark.md)
- [Mixin Messenger](../mixin.md)

完全なドキュメント一覧は [../README.md](../README.md) を参照してください。

## 開発

よく使うローカルコマンド：

```bash
./scripts/build-backend.sh --output ./bin/morph
./scripts/build-desktop.sh --release
go test ./...
```

Console フロントエンドは `web/console/` にあり、`pnpm` を使います。ローカルビルドの詳細は [../console.md](../console.md) と [../app.md](../app.md) を参照してください。

## 設定テンプレート

正規の設定テンプレートは [../../assets/config/config.example.yaml](../../assets/config/config.example.yaml) にあります。
環境変数は `MISTER_MORPH_` プレフィックスを使います。設定と代表的な flags の詳細は [../configuration.md](../configuration.md) にまとめています。
