# アイデア: skilldrift(仮)

## コンセプト

導入済みのClaude Code Skills/pluginの上流変更を監視し、diffが出たらリスクを自動再評価してPR/Issueを立てる「Skill版Dependabot」。

## 背景・動機

- Agent Skillsのサプライチェーン攻撃は既に実害段階(Snyk ToxicSkills調査: 3,984スキル中36%に欠陥・76個の確認済み悪意ペイロード・悪意スキルの91%がプロンプトインジェクション併用。ClawHavocインシデントも実在)。
- 既存の対策ツールは全て「導入前の一発スキャン」止まり。信頼済みスキルが**更新時に中身を変える**攻撃パターン(初回承認後のすり替え)に対する継続監視は空白地帯。
- Renovate/Dependabotの発想(依存の変更検知→自動評価→PR)をskillsエコシステムに輸入する。
- 動機: 実用(自分のskillsリポジトリでドッグフーディング)+ 公開(OSSとしてGitHubに出す)。

## ラフスコープ

- やる:
  - 導入済みskills/pluginの上流(元リポジトリ)の変更検知
  - 変更diffに対するリスク再評価(既存スキャナーのラップ + diff特化の評価)
  - GitHub ActionとしてCI組み込み、検知時にPR/Issue自動生成
  - Go製シングルバイナリで配布しやすく
- やらない(今回は):
  - 静的スキャンエンジンの自作(SkillSpector等をラップ。検出精度では戦わない)
  - ランタイム監視(sandbox+syscall/ネットワーク監視)— 将来の第2層
  - マルチエージェント統合監査ログ — 将来の第3層

## 競合・差別化(調査済み 2026-07-03)

- 静的スキャンは飽和: NVIDIA/SkillSpector 11.8k★、snyk/agent-scan 2.7k★、cisco-ai-defense/skill-scanner 2.3k★ほか多数。
- ドリフト監視は空白: 最近接のNPJigaK/skillspector-action(2★)はPR時の変更スキャンのみ、eric-sabe/honey(40★)は開発マシン向け日次watchdog、astra-sh/qvr(10★)はlockfile付きパッケージマネージャだがセキュリティ再評価なし。
- SkillSpector本体にwatch mode・ドリフト検知・lockfileはなし。exit code/JSONが安定契約なので検出エンジンとして利用可能。
- 判定: **競合あり・差別化可能** — 「導入前チェックの精度」ではなく「導入後の継続監視オーケストレーション」で差別化。

## 検討した他の候補

- ランタイム監視(consent gap対策のsandbox+syscall監視) — 技術的に面白いが実装が重く週末ペースに合わない。統合形の第2層として将来検討。
- マルチエージェント統合監査ログ — agmsgのSQLite基盤を流用できるがログフォーマット追従コストが継続的にかかる。第3層として将来検討。
- 最初から統合3層設計 — 初手として大きすぎる。ドリフト監視を第1マイルストーンに育てる方針に吸収。
