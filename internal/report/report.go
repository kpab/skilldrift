// Package report はドリフト検知結果をGitHub Issueとして報告する。
//
// 重複防止はIssue本文先頭の機械可読マーカーで行う:
//
//	<!-- skilldrift:skill=<name> fingerprint=<hash> -->
//
// Publish はopenなIssueを走査してマーカーを読み、
//   - 同一スキル・同一fingerprint → 何もしない(既報)
//   - 同一スキル・異なるfingerprint → 本文を更新し、コメントで通知
//   - 該当Issueなし → 新規作成
//
// とすることで、同じドリフトについてスキルあたり高々1つのopen Issueを保つ。
// fingerprintは上流の新commitと変更ファイル一覧から決まるため、
// 上流がさらに進めば同じIssueが更新され、closeすれば再検知時に新規作成される。
package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/kpab/skilldrift/internal/lockfile"
	"github.com/kpab/skilldrift/internal/upstream"
)

// Label は自動生成Issueに付けるラベル。
const Label = "skilldrift"

// Drift は1スキル分のドリフト検知結果。checkが組み立てPublishが報告する。
type Drift struct {
	Skill     string
	Repo      string // 上流 "owner/name"
	Subdir    string
	OldCommit string // lockfileに記録されていたcommit(未記入なら空)
	NewCommit string
	Changes   []lockfile.FileChange
	// NewHashes は変更ファイルの上流での現在のコンテンツハッシュ
	// (キーはChangesのPath。削除されたファイルは含まない)。Fingerprintの材料。
	NewHashes map[string]string

	// OldRisk/NewRisk は SkillSpector による新旧リスク評価(任意)。
	// スキャン未実施・未導入なら nil。旧=手元の現行スキル、新=上流の新版。
	// Fingerprintには含めない(内容ハッシュが同じなら再通知しないため)。
	OldRisk *Risk
	NewRisk *Risk
}

// Risk は SkillSpector のリスク評価を報告用に写した値。
type Risk struct {
	Score          int    // 0-100
	Severity       string // LOW / MEDIUM / HIGH / CRITICAL
	Recommendation string // SAFE / CAUTION / DO_NOT_INSTALL
}

// Fingerprint はドリフト内容の同一性判定に使うハッシュ(16桁hex)。
// 変更ファイルの一覧と内容が同じなら同じ値になる。上流のcommitは含めない:
// スキルに関係ないcommitで上流が進むたびにIssueを更新(通知)しないため。
func Fingerprint(d Drift) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s\n", d.Skill, d.Repo, d.Subdir)
	for _, c := range d.Changes {
		fmt.Fprintf(h, "%s %s %s\n", c.Kind, c.Path, d.NewHashes[c.Path])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Title はIssueのタイトル。
func Title(d Drift) string {
	return fmt.Sprintf("skilldrift: スキル %s の上流に変更を検知", d.Skill)
}

// marker はIssue本文に埋め込む機械可読マーカー。
func marker(d Drift) string {
	return fmt.Sprintf("<!-- skilldrift:skill=%s fingerprint=%s -->", d.Skill, Fingerprint(d))
}

var markerRe = regexp.MustCompile(`<!-- skilldrift:skill=(\S+) fingerprint=([0-9a-f]+) -->`)

// parseMarker はIssue本文からマーカーを読む。skilldrift製でなければ ok=false。
func parseMarker(body string) (skill, fingerprint string, ok bool) {
	m := markerRe.FindStringSubmatch(body)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// Body はIssue本文を組み立てる。
func Body(d Drift) string {
	var b strings.Builder
	b.WriteString(marker(d))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "スキル `%s` の上流に変更を検知した。取り込む前に、悪意ある変更(プロンプトインジェクション、不審なスクリプトやURLの追加など)が無いか内容を確認すること。\n\n", d.Skill)

	fmt.Fprintf(&b, "- 上流: [`%s`](https://github.com/%s)", d.Repo, d.Repo)
	if d.Subdir != "" {
		fmt.Fprintf(&b, " の `%s`", d.Subdir)
	}
	b.WriteString("\n")
	if d.OldCommit != "" {
		fmt.Fprintf(&b, "- commit: `%s` → `%s`([diffを見る](https://github.com/%s/compare/%s...%s))\n",
			short(d.OldCommit), short(d.NewCommit), d.Repo, d.OldCommit, d.NewCommit)
	} else {
		fmt.Fprintf(&b, "- commit: (lockfileに未記録)→ `%s`\n", short(d.NewCommit))
	}

	b.WriteString("\n### 変更ファイル\n\n")
	for _, c := range d.Changes {
		fmt.Fprintf(&b, "- %s: `%s`\n", changeLabel(c.Kind), c.Path)
	}

	b.WriteString(riskSection(d))

	b.WriteString("\n### 対応\n\n")
	b.WriteString("問題ない変更なら、ローカルのスキルを上流に合わせて更新し `skilldrift init` を再実行するとlockfileが現状で更新され、このIssueの根拠は解消する。\n")
	b.WriteString("\n---\n*このIssueは [skilldrift](https://github.com/kpab/skilldrift) が自動生成した。同じスキルの上流がさらに変わると本文を更新する。*\n")
	return b.String()
}

// riskSection は新旧のリスク評価をバッジで色分けした表として描画する。
// 新版がHIGH以上(またはDO_NOT_INSTALL)なら、表の前に専用の幅広バナー枠を出す。
// NewRisk が nil(スキャン未実施)なら空文字を返し、本文に節を出さない。
func riskSection(d Drift) string {
	if d.NewRisk == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n### リスク再評価(SkillSpector)\n\n")

	if highRisk(d.NewRisk) {
		fmt.Fprintf(&b, "%s %s\n\n",
			bannerBadge("RISK", fmt.Sprintf("%s %d/100", strings.ToUpper(d.NewRisk.Severity), d.NewRisk.Score), sevColor(d.NewRisk.Severity)),
			bannerBadge("RECOMMENDATION", d.NewRisk.Recommendation, recColor(d.NewRisk.Recommendation)))
	}

	b.WriteString("| 指標 | 現行(手元) | 上流の新版 |\n")
	b.WriteString("|------|------------|------------|\n")
	fmt.Fprintf(&b, "| スコア | %s | %d |\n", scoreCell(d.OldRisk), d.NewRisk.Score)
	fmt.Fprintf(&b, "| 深刻度 | %s | %s |\n", sevCell(d.OldRisk), sevBadge(d.NewRisk.Severity))
	fmt.Fprintf(&b, "| 推奨 | %s | %s |\n", recCell(d.OldRisk), recBadge(d.NewRisk.Recommendation))

	if worsened(d.OldRisk, d.NewRisk) {
		fmt.Fprintf(&b, "\n> [!CAUTION]\n> **上流の新版はリスク評価が悪化している(スコア %d → %d、推奨 %s)。**\n> 取り込みは特に慎重に確認すること。\n",
			d.OldRisk.Score, d.NewRisk.Score, d.NewRisk.Recommendation)
	}
	return b.String()
}

// highRisk は専用のバナー枠を出すべき高リスク評価かを返す。
func highRisk(r *Risk) bool {
	return sevRank(r.Severity) >= sevRank("HIGH") || recRank(r.Recommendation) >= recRank("DO_NOT_INSTALL")
}

// ---- shields.io バッジ描画 ----
// 画像が読めない環境でもalt文字列だけで内容が伝わるようにする。

// badgeEscape は shields.io の静的バッジのパス断片向けエスケープ。
// ("-" は区切り文字なので "--"、"_" は空白扱いなので "__" に逃がす)
func badgeEscape(s string) string {
	return url.PathEscape(strings.NewReplacer("-", "--", "_", "__", " ", "_").Replace(s))
}

// mdBadge は色付きの小バッジ1個分のMarkdown。表のセル用。
func mdBadge(message, color string) string {
	return fmt.Sprintf("![%s](https://img.shields.io/badge/%s-%s?style=flat-square)",
		message, badgeEscape(message), color)
}

// bannerBadge は高リスク専用の幅広バッジ。ラベル付き・for-the-badgeスタイルで
// 通常の表より一回り大きく描画される。
func bannerBadge(label, message, color string) string {
	return fmt.Sprintf("![%s: %s](https://img.shields.io/badge/%s-%s-%s?style=for-the-badge&labelColor=24292f)",
		label, message, badgeEscape(label), badgeEscape(message), color)
}

// sevColor / recColor は shields.io の色名へのマッピング。
func sevColor(s string) string {
	switch strings.ToUpper(s) {
	case "LOW":
		return "success"
	case "MEDIUM":
		return "yellow"
	case "HIGH":
		return "orange"
	case "CRITICAL":
		return "critical"
	default:
		return "lightgrey"
	}
}

func recColor(s string) string {
	switch strings.ToUpper(s) {
	case "SAFE":
		return "success"
	case "CAUTION":
		return "yellow"
	case "DO_NOT_INSTALL":
		return "critical"
	default:
		return "lightgrey"
	}
}

func sevBadge(s string) string { return mdBadge(s, sevColor(s)) }
func recBadge(s string) string { return mdBadge(s, recColor(s)) }

func scoreCell(r *Risk) string {
	if r == nil {
		return "—"
	}
	return fmt.Sprintf("%d", r.Score)
}

func sevCell(r *Risk) string {
	if r == nil {
		return "—"
	}
	return sevBadge(r.Severity)
}

func recCell(r *Risk) string {
	if r == nil {
		return "—"
	}
	return recBadge(r.Recommendation)
}

// worsened は新版のリスクが旧版より悪化したかを返す。
// スコア増加・深刻度上昇・推奨の悪化のいずれかがあれば true。
func worsened(old, new *Risk) bool {
	if old == nil || new == nil {
		return false
	}
	return new.Score > old.Score ||
		sevRank(new.Severity) > sevRank(old.Severity) ||
		recRank(new.Recommendation) > recRank(old.Recommendation)
}

func sevRank(s string) int {
	switch strings.ToUpper(s) {
	case "LOW":
		return 1
	case "MEDIUM":
		return 2
	case "HIGH":
		return 3
	case "CRITICAL":
		return 4
	default:
		return 0
	}
}

func recRank(s string) int {
	switch strings.ToUpper(s) {
	case "SAFE":
		return 1
	case "CAUTION":
		return 2
	case "DO_NOT_INSTALL":
		return 3
	default:
		return 0
	}
}

func changeLabel(k lockfile.ChangeKind) string {
	switch k {
	case lockfile.ChangeAdded:
		return "追加"
	case lockfile.ChangeRemoved:
		return "削除"
	default:
		return "変更"
	}
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// API は Publish が必要とするGitHub Issue操作。*upstream.Client が満たす。
type API interface {
	ListOpenIssues(ctx context.Context, repo string) ([]upstream.Issue, error)
	CreateIssue(ctx context.Context, repo, title, body string, labels []string) (upstream.Issue, error)
	UpdateIssue(ctx context.Context, repo string, number int, title, body string) error
	CommentIssue(ctx context.Context, repo string, number int, body string) error
}

// Action はPublishが各ドリフトに対して取った操作。
type Action string

const (
	ActionCreated Action = "created" // 新規Issueを作成した
	ActionUpdated Action = "updated" // 既存Issueを新しいドリフト内容で更新した
	ActionSkipped Action = "skipped" // 同一内容のopen Issueが既にある(既報)
)

// Published は1ドリフト分の報告結果。
type Published struct {
	Skill  string
	Number int // Issue番号
	Action Action
}

// Publish は drifts を repo のIssueとして報告する。途中で失敗した場合、
// そこまでの結果とエラーを返す(部分的に作成済みのIssueはそのまま残る)。
func Publish(ctx context.Context, api API, repo string, drifts []Drift) ([]Published, error) {
	open, err := api.ListOpenIssues(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("既存Issueの取得に失敗: %w", err)
	}
	type existing struct {
		number      int
		fingerprint string
	}
	bySkill := map[string]existing{}
	for _, is := range open {
		if skill, fp, ok := parseMarker(is.Body); ok {
			bySkill[skill] = existing{number: is.Number, fingerprint: fp}
		}
	}

	var results []Published
	for _, d := range drifts {
		ex, found := bySkill[d.Skill]
		switch {
		case found && ex.fingerprint == Fingerprint(d):
			results = append(results, Published{Skill: d.Skill, Number: ex.number, Action: ActionSkipped})
		case found:
			if err := api.UpdateIssue(ctx, repo, ex.number, Title(d), Body(d)); err != nil {
				return results, fmt.Errorf("Issue #%d の更新に失敗: %w", ex.number, err)
			}
			comment := fmt.Sprintf("上流がさらに更新された(現在のcommit: `%s`)。本文を最新のドリフト内容に更新した。", short(d.NewCommit))
			if err := api.CommentIssue(ctx, repo, ex.number, comment); err != nil {
				return results, fmt.Errorf("Issue #%d へのコメントに失敗: %w", ex.number, err)
			}
			results = append(results, Published{Skill: d.Skill, Number: ex.number, Action: ActionUpdated})
		default:
			is, err := api.CreateIssue(ctx, repo, Title(d), Body(d), []string{Label})
			if err != nil {
				return results, fmt.Errorf("スキル %s のIssue作成に失敗: %w", d.Skill, err)
			}
			results = append(results, Published{Skill: d.Skill, Number: is.Number, Action: ActionCreated})
		}
	}
	return results, nil
}
