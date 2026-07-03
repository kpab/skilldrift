# skilldrift 要件

## 背景・目的

Claude Code Skillsのサプライチェーン攻撃は実害段階に入ったが、既存対策は「導入前の一発スキャン」止まりで、信頼済みスキルが更新時に中身を変える攻撃(初回承認後のすり替え)への継続監視は空白。Dependabot/Renovateの発想をskillsエコシステムに輸入する。詳細は docs/IDEA.md 参照。

**成功条件**: 自分の公開skillsリポジトリにGitHub Actionとして導入し、参照している上流スキルの変更を検知してリスク評価付きIssueが自動で立つ。

## ユーザー

- まず自分(skillsリポジトリでドッグフーディング)
- 公開OSSとして、skillsをリポジトリで管理する開発者全般

## スコープ

### やること(MVP)

- **lockfile管理**: `skilldrift.lock` に各スキルの出自(上流repo+commit+コンテンツハッシュ)を記録。`init`で生成、以後これが真の情報源
- **ドリフト検知**: 上流リポジトリの現在の状態とlockfileを比較し、変更(diff)を検出
- **リスク再評価とIssue生成**: 変更されたスキルを既存スキャナー(SkillSpector)で再スキャンし、新旧比較+diff要約付きのIssueをGitHub Actionから自動生成

### やらないこと(今回は)

- 静的スキャンエンジンの自作(SkillSpectorをラップ。検出精度では戦わない)
- PR自動生成(悪意ある更新を取り込む導線になり得るため、安全設計してからv2で)
- ランタイム監視(sandbox+syscall/ネットワーク監視)— 将来の第2層
- マルチエージェント統合監査ログ — 将来の第3層
- ローカル環境(~/.claude/skills)の監視 — CLIコアは共通化するが、MVPはリポジトリ内スキルのみ対象
- marketplace/plugin設定からの出自自動推定(lockfileへの手動記入で開始)

## 技術選定

| 項目 | 選定 | 理由 |
|------|------|------|
| 言語 | Go | 主戦場。シングルバイナリでcomposite actionから使いやすい |
| CLI | cobra または stdlib flag | サブコマンド(init/check)構成。実装時に確定 |
| 配布 | GitHub Releases + goreleaser | composite actionがreleaseバイナリをDLする形態 |
| スキャナー | NVIDIA SkillSpector | exit code/JSONが安定契約。CI上でpip等で導入 |
| フロントエンド | なし | CLIツール+GitHub Action |
| インフラ | GitHub Actions のみ | サーバー不要。定期実行はschedule trigger |

## 公開・運用

- リポジトリ: public / ライセンス: MIT
- デプロイ先: 利用者のGitHub Actions上(composite action)+ ローカルCLI
- CI: あり — 自身のテスト+goreleaserでのリリース。ドッグフーディングとして自分のskillsリポジトリに導入

## マイルストーン

- **M1(最小の動くもの)**: `skilldrift init` と `skilldrift check` がローカルで動く
  — lockfileを生成し、上流に変更があれば「どのスキルがどう変わったか」をターミナルに出せる。
  検証方法: 自分のskillsリポジトリのクローンで init → 上流の古いcommitをlockに指定 → check でdiff検出を確認
  - [x] lockfileスキーマを設計し、Go構造体+読み書き(internal/lockfile)を実装。変更粒度(commit vs コンテンツハッシュ)もここで確定
  - [x] `init`: スキルディレクトリを走査し、出自未記入のエントリを持つlockfileを生成(出自は手で埋める前提)
  - [x] 上流取得: lockfileのrepo参照から現在のcommit/ファイル内容を取得(gh CLIまたはGitHub API)
  - [ ] `check`: lockfileと上流を比較し、変わったスキルとdiff要約をターミナル出力。終了コードで有無を表現
  - [ ] 実データ検証: 自分のskillsリポジトリで古いcommitを指定してドリフト検出を確認
- **M2(CI統合)**: composite action化 + Issue自動生成。自分のskillsリポジトリにschedule実行で導入し、実際にIssueが立つところまで
- **M3(リスク評価)**: SkillSpectorラップによる新旧スコア比較をIssue本文に組み込む。goreleaserでv0.1.0公開

## 将来(今は設計だけ意識)

- PR自動生成(安全と判定された更新のみ)
- ランタイム監視レイヤー(consent gap対策)
- マルチエージェント統合監査ログ(agmsg SQLite基盤の流用)
- lockfileスキーマはスキル以外(MCP設定・plugin)にも拡張できる形にしておく

## 決定事項

- **変更検知の粒度はハイブリッド**(M1で確定): 最終判定はファイル単位のコンテンツハッシュ(sha256)。commit単位だと上流のスキル外変更で誤検知し、force-push等でcommitが動かないすり替えも見逃すため。commitは`check`の短絡判定(一致なら取得省略)とdrift時のdiff起点として併記する。SKILL.md以外の同梱ファイル(スクリプト等)もスキルディレクトリ配下を再帰的に全て対象とする。スキーマの詳細は `internal/lockfile` のパッケージコメント参照

- **上流取得はGitHub API直叩き**(M1で確定): gh CLIに依存すると利用者の導入前提が増えるため、標準ライブラリのみでREST APIを叩く。認証は`GITHUB_TOKEN`/`GH_TOKEN`(任意)。ファイル内容はtarball APIでリポジトリ×commitあたり1リクエストにまとめ、匿名rate limit(60回/時)でも実用にする

## 未定事項

- CLIフレームワーク(cobra vs stdlib)— M1実装時に決定
- SkillSpectorのCI上での導入方法(pip / uvx / バイナリ)— M3で調査
- Issueの重複防止(同じdriftで毎回立てない仕組み)— M2で設計
- Action名・marketplace公開するか — M2以降
