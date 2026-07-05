# skilldrift 要件

## 背景・目的

Claude Code Skillsのサプライチェーン攻撃は実害段階に入ったが、既存対策は「導入前の一発スキャン」止まりで、信頼済みスキルが更新時に中身を変える攻撃(初回承認後のすり替え)への継続監視は空白。Dependabot/Renovateの発想をskillsエコシステムに輸入する。詳細は docs/IDEA.md 参照。

**成功条件**: 第三者のスキルをvendorしたリポジトリにGitHub Actionとして導入し、参照している上流スキルの変更を検知してリスク評価付きIssueが自動で立つ。(当初は「自分の公開skillsリポジトリ」を想定したが、自作リポジトリでは上流=自分自身の自己参照になり通知価値がないと判明したため、第三者スキルのvendorへ修正。M3で `kpab/skilldrift-vendor-lab` にて達成)

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
  - [x] `check`: lockfileと上流を比較し、変わったスキルとdiff要約をターミナル出力。終了コードで有無を表現
  - [x] 実データ検証: 自分のskillsリポジトリで古いcommitを指定してドリフト検出を確認
    (kpab/claude-fable-5-skills のクローンで cbb4851 を指定 → effort-calibrator の SKILL.md 変更を検知、
    他9スキルは「commit進行のみ・内容同一」と正しく判定。git diff --stat と完全一致)
- **M2(CI統合)**: composite action化 + Issue自動生成。自分のskillsリポジトリにschedule実行で導入し、実際にIssueが立つところまで
  - [x] `check -issue`: ドリフトをGitHub Issueとして報告(internal/report)。重複防止つき
  - [x] tarball取得のキャッシュ(同一repo×commitは1回だけ取得)
  - [x] composite action(action.yml)。当面はactionのref時点のソースをgo buildする方式
  - [x] 実環境検証: kpab/claude-fable-5-skills にschedule導入し、古いcommit(cbb4851)基準の
    lockfileで effort-calibrator のドリフト検知→Issue #2 自動生成を確認。再実行で既報スキップ、
    fingerprint変化時は本文更新+コメント通知になることも実runで確認済み。
    検証後に導入は撤去した(自分が作者のリポジトリでは上流=自分自身の自己参照になり、
    通知されるのは自分のコミットだけで実用価値がないため)。実利用としてのドッグフーディングは
    第三者のOSSスキルをvendorするリポジトリで行う(M3の検証と合わせて用意する)
- **M3(リスク評価)**: SkillSpectorラップによる新旧スコア比較をIssue本文に組み込む。goreleaserでv0.1.0公開
  - [x] `internal/riskscan`: skillspector CLIをラップ(`scan --format json --no-llm`)。exit 0/1は正常JSON・2はエラー
  - [x] `check -scan`(既定ON・未導入なら自動スキップ): ドリフト時に旧=手元の現行dir・新=上流の取得ファイルを再スキャンしDriftに付与
  - [x] Issue本文に新旧リスク比較(スコア/深刻度/推奨の表+悪化警告)を組み込む。fingerprintには含めない
  - [x] action.yml: `uv`でskillspectorを導入し `scan-risk` 入力(既定true)を追加
  - [x] goreleaser設定(.goreleaser.yaml)と tags:v* でリリースするworkflowを追加。
    `goreleaser check` と `--snapshot` ビルドでアーカイブ名・version注入を検証済み
  - [x] action.ymlをreleaseバイナリDL方式へ切替(version入力 > actionのref > 最新releaseで解決)
  - [x] `v0.1.0` タグを打って初回リリースを公開。release workflowが成功し、6プラットフォームの
    バイナリ+checksums.txtを生成。`releases/latest/download/` 経路のDL・展開が実際に動くことを確認
  - [x] GitHub Marketplace公開(v0.1.2)。actionのname衝突(既存user `skilldrift`)を一意な
    表示名へ、descriptionを125文字制限に収め、英語化。listing: Skilldrift — Skills Drift Monitor
  - [x] **第三者スキルでの実利用ドッグフーディング完了**(成功条件の達成)。検証用public repo
    `kpab/skilldrift-vendor-lab` を作成し、公開済みmarketplace action(kpab/skilldrift@v0.1.2)を
    schedule/dispatchで運用。2モードで全パス確認:
    - Mode A(本物ドリフト): 第三者 `ljagiello/ctf-skills`(MIT)から ctf-web/crypto/pwn を
      古いcommit基準でvendor → 上流HEADとの差分をIssue自動生成。再runで既報スキップ(fingerprint重複防止)も実証
    - Mode B(リスク悪化検知): fork `kpab/ctf-skills` に良性スキル safe-formatter を置き、
      疑わしいスクリプト(curl|bash+env exfil、宛先example.comの無害デモ)を注入 → SkillSpectorスコア
      46/MEDIUM/CAUTION → 68/HIGH/DO_NOT_INSTALL の悪化をIssue本文の比較表+警告で表示
    - 知見: リスク悪化デモにCTFスキルは不適。攻撃技法スキルは素で100/CRITICALに振り切れており
      delta が見えないため、素が低〜中リスクの良性スキルで行う必要がある

## 将来(今は設計だけ意識)

- PR自動生成(安全と判定された更新のみ)
- ランタイム監視レイヤー(consent gap対策)
- マルチエージェント統合監査ログ(agmsg SQLite基盤の流用)
- lockfileスキーマはスキル以外(MCP設定・plugin)にも拡張できる形にしておく

## 決定事項

- **変更検知の粒度はハイブリッド**(M1で確定): 最終判定はファイル単位のコンテンツハッシュ(sha256)。commit単位だと上流のスキル外変更で誤検知し、force-push等でcommitが動かないすり替えも見逃すため。commitは`check`の短絡判定(一致なら取得省略)とdrift時のdiff起点として併記する。SKILL.md以外の同梱ファイル(スクリプト等)もスキルディレクトリ配下を再帰的に全て対象とする。スキーマの詳細は `internal/lockfile` のパッケージコメント参照

- **checkの終了コードは 0=ドリフトなし / 1=ドリフト検知 / 2=エラー**(M1で確定): grep等の慣例に合わせ、CIで「ドリフト」と「実行失敗」を区別できるようにする
- **Issueの重複防止はマーカー方式**(M2で確定): Issue本文先頭に機械可読マーカー
  `<!-- skilldrift:skill=<name> fingerprint=<hash> -->` を埋め、実行時にopen Issueを走査して
  同一スキル・同一fingerprint(変更ファイル一覧+各ファイルの新コンテンツハッシュ。
  上流commitは含めない — スキルと無関係なcommitで上流が進むたびに通知しないため)なら何もしない。
  fingerprintが変わっていたら本文を更新しコメントで通知する(スキルあたりopen Issueは高々1つ)。
  ラベルでの検索に依存しないのは、ラベル未作成のリポジトリでも壊れないようにするため
  (`skilldrift` ラベルは人間向けのフィルタとして付与はする)

- **リスク再評価の基準は「手元の現行版 vs 上流の新版」**(M3で確定): 旧側は上流の旧commitを再取得せず、
  リポジトリにコミット済みのローカルのスキルディレクトリをそのままスキャンする。=利用者が現に信頼している状態と、
  上流が今提示している新版の差分こそが評価したいリスクデルタだから。追加のtarball取得も不要。
  リスク評価はfingerprintに含めない(内容ハッシュが同じなら再通知しない設計を崩さないため。
  スコアはスキャナーのバージョンや非決定性で揺れうるので同一性判定に使うのは不適切)。
  スキャンは決定的でAPIキー不要な `--no-llm`(静的解析のみ)で行う

- **composite actionはreleaseバイナリDL方式**(M3で切替): goreleaserが命名した
  `skilldrift_<goos>_<goarch>.tar.gz` を GitHub Releases から `curl` で取得して実行する。
  取得バージョンは version 入力 > actionのref(vX.Y.Z形式のとき) > 最新release の順で解決。
  M2まではreleaseが無かったため `actions/setup-go` + `go build` の暫定方式だった

- **上流取得はGitHub API直叩き**(M1で確定): gh CLIに依存すると利用者の導入前提が増えるため、標準ライブラリのみでREST APIを叩く。認証は`GITHUB_TOKEN`/`GH_TOKEN`(任意)。ファイル内容はtarball APIでリポジトリ×commitあたり1リクエストにまとめ、匿名rate limit(60回/時)でも実用にする

## 未定事項

- CLIフレームワーク(cobra vs stdlib)— M1実装時に決定 → stdlib flagで確定
- SkillSpectorのCI上での導入方法(pip / uvx / バイナリ)— M3で調査 → `uv tool install`(公式推奨)で確定。
  actionは `astral-sh/setup-uv` + `uv tool install git+https://github.com/NVIDIA/skillspector.git`。
  CLIは skillspector をPATHから解決し、不在なら静かにスキップする(ローカル利用者に導入を強制しない)
- Action名・marketplace公開するか — M3以降
