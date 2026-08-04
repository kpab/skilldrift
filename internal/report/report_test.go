package report

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kpab/skilldrift/internal/lockfile"
	"github.com/kpab/skilldrift/internal/upstream"
)

func sampleDrift() Drift {
	return Drift{
		Skill:     "effort-calibrator",
		Repo:      "owner/skills",
		Subdir:    "skills/effort-calibrator",
		OldCommit: strings.Repeat("a", 40),
		NewCommit: strings.Repeat("b", 40),
		Changes: []lockfile.FileChange{
			{Path: "SKILL.md", Kind: lockfile.ChangeModified},
			{Path: "scripts/new.sh", Kind: lockfile.ChangeAdded},
		},
		NewHashes: map[string]string{
			"SKILL.md":       "sha256:1111",
			"scripts/new.sh": "sha256:2222",
		},
	}
}

func TestFingerprint(t *testing.T) {
	d := sampleDrift()
	if Fingerprint(d) != Fingerprint(sampleDrift()) {
		t.Error("同じドリフトのfingerprintが一致しない")
	}

	// スキルと無関係なcommitで上流が進んでも、変更内容が同じなら同一視する
	moved := sampleDrift()
	moved.NewCommit = strings.Repeat("c", 40)
	if Fingerprint(d) != Fingerprint(moved) {
		t.Error("変更内容が同じならcommitが進んでもfingerprintは変わらないはず")
	}

	changed := sampleDrift()
	changed.Changes = changed.Changes[:1]
	if Fingerprint(d) == Fingerprint(changed) {
		t.Error("変更ファイル一覧が違えばfingerprintも変わるはず")
	}

	rewritten := sampleDrift()
	rewritten.NewHashes["SKILL.md"] = "sha256:9999"
	if Fingerprint(d) == Fingerprint(rewritten) {
		t.Error("変更ファイルの内容が違えばfingerprintも変わるはず")
	}
}

func TestBodyMarkerRoundTrip(t *testing.T) {
	d := sampleDrift()
	body := Body(d)

	skill, fp, ok := parseMarker(body)
	if !ok {
		t.Fatalf("本文からマーカーを読めない:\n%s", body)
	}
	if skill != d.Skill || fp != Fingerprint(d) {
		t.Errorf("marker = (%q, %q), want (%q, %q)", skill, fp, d.Skill, Fingerprint(d))
	}

	// 人間向けの中身も最低限入っていること
	for _, want := range []string{
		"effort-calibrator",
		"compare/" + d.OldCommit + "..." + d.NewCommit,
		"`SKILL.md`",
		"`scripts/new.sh`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("本文に %q が無い", want)
		}
	}
}

func TestBodyRiskSection(t *testing.T) {
	d := sampleDrift()
	d.OldRisk = &Risk{Score: 12, Severity: "LOW", Recommendation: "SAFE"}
	d.NewRisk = &Risk{Score: 55, Severity: "HIGH", Recommendation: "CAUTION"}
	body := Body(d)

	for _, want := range []string{
		"リスク再評価(SkillSpector)",
		"| スコア | 12 | 55 |",
		"badge/LOW-success",   // 旧深刻度のバッジ
		"badge/HIGH-orange",   // 新深刻度のバッジ
		"badge/SAFE-success",  // 旧推奨のバッジ
		"badge/CAUTION-yellow",
		"style=for-the-badge", // HIGHなので専用バナー枠が出る
		"[!CAUTION]",          // 悪化しているので警告枠が出る
	} {
		if !strings.Contains(body, want) {
			t.Errorf("本文に %q が無い:\n%s", want, body)
		}
	}
}

func TestBodyRiskBannerOnlyForHighRisk(t *testing.T) {
	// MEDIUM/CAUTION止まりなら幅広バナー枠は出さない
	d := sampleDrift()
	d.OldRisk = &Risk{Score: 10, Severity: "LOW", Recommendation: "SAFE"}
	d.NewRisk = &Risk{Score: 40, Severity: "MEDIUM", Recommendation: "CAUTION"}
	if strings.Contains(Body(d), "for-the-badge") {
		t.Error("MEDIUMなのに高リスク専用バナーが出ている")
	}

	// DO_NOT_INSTALLなら深刻度に関わらずバナーを出す
	d.NewRisk = &Risk{Score: 40, Severity: "MEDIUM", Recommendation: "DO_NOT_INSTALL"}
	body := Body(d)
	if !strings.Contains(body, "for-the-badge") {
		t.Errorf("DO_NOT_INSTALLなのに高リスク専用バナーが出ない:\n%s", body)
	}
	if !strings.Contains(body, "badge/RECOMMENDATION-DO__NOT__INSTALL-critical") {
		t.Errorf("推奨バナーのエスケープ・色が想定と違う:\n%s", body)
	}
}

func TestBodyRiskSectionOmittedWhenNoScan(t *testing.T) {
	body := Body(sampleDrift()) // NewRisk == nil
	if strings.Contains(body, "リスク再評価") {
		t.Error("スキャン未実施なのにリスク節が出ている")
	}
}

func TestBodyRiskSectionNoWarningWhenNotWorse(t *testing.T) {
	d := sampleDrift()
	d.OldRisk = &Risk{Score: 40, Severity: "MEDIUM", Recommendation: "CAUTION"}
	d.NewRisk = &Risk{Score: 20, Severity: "LOW", Recommendation: "SAFE"}
	if strings.Contains(Body(d), "[!CAUTION]") {
		t.Error("リスクが下がっているのに警告が出ている")
	}
}

func TestBodyRiskSectionOldMissing(t *testing.T) {
	// 手元のスキャンが失敗して旧側が無い場合は "—" で描く
	d := sampleDrift()
	d.NewRisk = &Risk{Score: 30, Severity: "MEDIUM", Recommendation: "CAUTION"}
	body := Body(d)
	if !strings.Contains(body, "| スコア | — | 30 |") {
		t.Errorf("旧側欠落時の描画になっていない:\n%s", body)
	}
}

func TestFingerprintIgnoresRisk(t *testing.T) {
	d := sampleDrift()
	withRisk := sampleDrift()
	withRisk.OldRisk = &Risk{Score: 1, Severity: "LOW", Recommendation: "SAFE"}
	withRisk.NewRisk = &Risk{Score: 99, Severity: "CRITICAL", Recommendation: "DO_NOT_INSTALL"}
	if Fingerprint(d) != Fingerprint(withRisk) {
		t.Error("リスク評価はfingerprintに影響してはいけない")
	}
}

func TestParseMarkerNonSkilldriftIssue(t *testing.T) {
	if _, _, ok := parseMarker("ふつうの手書きIssue本文"); ok {
		t.Error("マーカーの無い本文でok=trueになった")
	}
}

// fakeAPI は Publish のテスト用。呼び出しを記録する。
type fakeAPI struct {
	open     []upstream.Issue
	created  []string // 作成したIssueのタイトル
	updated  []int    // 更新したIssue番号
	comments []int    // コメントしたIssue番号
	fail     error
}

func (f *fakeAPI) ListOpenIssues(ctx context.Context, repo string) ([]upstream.Issue, error) {
	return f.open, f.fail
}

func (f *fakeAPI) CreateIssue(ctx context.Context, repo, title, body string, labels []string) (upstream.Issue, error) {
	f.created = append(f.created, title)
	return upstream.Issue{Number: 100 + len(f.created), Title: title, Body: body}, nil
}

func (f *fakeAPI) UpdateIssue(ctx context.Context, repo string, number int, title, body string) error {
	f.updated = append(f.updated, number)
	return nil
}

func (f *fakeAPI) CommentIssue(ctx context.Context, repo string, number int, body string) error {
	f.comments = append(f.comments, number)
	return nil
}

func TestPublishCreatesNewIssue(t *testing.T) {
	api := &fakeAPI{}
	results, err := Publish(context.Background(), api, "me/skills", []Drift{sampleDrift()})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(results) != 1 || results[0].Action != ActionCreated {
		t.Fatalf("results = %+v", results)
	}
	if len(api.created) != 1 {
		t.Errorf("created = %v", api.created)
	}
}

func TestPublishSkipsSameFingerprint(t *testing.T) {
	d := sampleDrift()
	api := &fakeAPI{open: []upstream.Issue{{Number: 5, Body: Body(d)}}}

	results, err := Publish(context.Background(), api, "me/skills", []Drift{d})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(results) != 1 || results[0].Action != ActionSkipped || results[0].Number != 5 {
		t.Fatalf("results = %+v", results)
	}
	if len(api.created)+len(api.updated)+len(api.comments) != 0 {
		t.Errorf("既報のIssueに書き込みが発生した: %+v", api)
	}
}

func TestPublishUpdatesOnNewFingerprint(t *testing.T) {
	old := sampleDrift()
	api := &fakeAPI{open: []upstream.Issue{{Number: 5, Body: Body(old)}}}

	moved := sampleDrift()
	moved.NewCommit = strings.Repeat("c", 40)
	moved.NewHashes["SKILL.md"] = "sha256:9999" // 上流でさらに書き換わった
	results, err := Publish(context.Background(), api, "me/skills", []Drift{moved})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(results) != 1 || results[0].Action != ActionUpdated || results[0].Number != 5 {
		t.Fatalf("results = %+v", results)
	}
	if len(api.updated) != 1 || len(api.comments) != 1 {
		t.Errorf("本文更新+コメントを期待したが: updated=%v comments=%v", api.updated, api.comments)
	}
	if len(api.created) != 0 {
		t.Error("既存Issueがあるのに新規作成された")
	}
}

func TestPublishListFailure(t *testing.T) {
	api := &fakeAPI{fail: errors.New("boom")}
	if _, err := Publish(context.Background(), api, "me/skills", []Drift{sampleDrift()}); err == nil {
		t.Error("一覧取得失敗でエラーになるはず")
	}
}
