# skilldrift

Dependabot for Claude Code Skills — 導入済みスキルの上流変更を監視し、ドリフトを検知してリスク再評価付きIssueを立てるCLI + GitHub Action。

既存のスキルスキャナーが「導入前の一発スキャン」で止まるのに対し、skilldriftは**導入後**を見張る: 信頼して入れたスキルが更新で中身を変えたとき、それに気付けるようにする。

## 使い方

### CLI

```sh
# スキルの出自を skilldrift.lock に記録(source.repo / commit は手で記入する)
skilldrift init

# 上流との差分を検知(終了コード: 0=ドリフトなし / 1=ドリフト検知 / 2=エラー)
skilldrift check

# ドリフト検知時にGitHub Issueも作成/更新する
GITHUB_TOKEN=... skilldrift check -issue -issue-repo owner/repo
```

### GitHub Action

スキルを管理しているリポジトリに `skilldrift.lock` をコミットした上で、workflowを置く:

```yaml
name: skilldrift
on:
  schedule:
    - cron: "0 0 * * *" # 毎日 09:00 JST
  workflow_dispatch:

permissions:
  contents: read
  issues: write

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: kpab/skilldrift@main
```

ドリフトを検知するとスキルごとにIssueが立つ。同じ内容のopen Issueがある間は重複して立てず、上流がさらに進んだ場合は既存Issueの本文を更新してコメントで通知する。

## ステータス

開発中(M2: CI統合まで完了)。計画は docs/REQUIREMENTS.md を参照。

## License

MIT
