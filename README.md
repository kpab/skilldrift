# skilldrift

Dependabot for Claude Code Skills — 導入済みスキルの上流変更を監視し、ドリフトを検知してリスク再評価付きIssueを立てるCLI + GitHub Action。

既存のスキルスキャナーが「導入前の一発スキャン」で止まるのに対し、skilldriftは**導入後**を見張る: 信頼して入れたスキルが更新で中身を変えたとき、それに気付けるようにする。

## 使い方(予定)

```sh
# スキルの出自を skilldrift.lock に記録
skilldrift init

# 上流との差分を検知
skilldrift check
```

GitHub Actionとしてschedule実行し、ドリフト検知時にIssueを自動生成する(M2以降)。

## ステータス

開発初期。計画は docs/REQUIREMENTS.md を参照。

## License

MIT
