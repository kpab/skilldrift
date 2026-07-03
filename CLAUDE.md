# skilldrift

導入済みClaude Code Skillsの上流変更を監視する「Skill版Dependabot」。
lockfileで出自を追跡し、ドリフト検知→リスク再評価→Issue自動生成までを行うGo製CLI + GitHub Action。

要件・計画の詳細は docs/REQUIREMENTS.md、アイデアの経緯は docs/IDEA.md を参照。

## 技術スタック

- Go(シングルバイナリ、goreleaserで配布)
- GitHub Actions(composite action。releaseバイナリをDLして実行)
- 外部スキャナー: NVIDIA SkillSpector(ラップして使う。検出精度では戦わない)

## ディレクトリ構成

- `main.go` — CLIエントリポイント(サブコマンド: init / check / version)。標準flagベース
- `docs/` — IDEA.md(経緯)、REQUIREMENTS.md(要件・マイルストーン)
- ロジックが育ったら `internal/` に切り出す(lockfile・上流取得・diff検知)

## コマンド

```sh
go build ./...   # ビルド
go test ./...    # テスト
go run . check   # 動作確認
```

## 規約・注意点

- lockfile(`skilldrift.lock`)が真の情報源。lockfileのスキーマ変更は後方互換を壊さないこと(将来MCP設定・pluginにも拡張予定)
- PR自動生成は意図的にスコープ外(悪意ある更新の取り込み導線になるため)。実装しない
- コミットメッセージは日本語
