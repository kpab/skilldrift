// Package riskscan は外部スキャナー NVIDIA SkillSpector をラップし、
// スキルディレクトリのリスク評価(スコア・深刻度・推奨)を取得する。
//
// 検出精度では戦わない方針のため独自の静的解析は持たず、skillspector CLI を
// サブプロセスとして呼ぶだけにする。CIでは決定的で速くAPIキー不要な
// --no-llm(静的解析のみ)で実行する。
//
// skillspector の終了コードは 0=score≤50 / 1=score>50 / 2=エラー で、
// 0と1のどちらでも risk_assessment を含むJSONを標準出力に出す。
// このパッケージは 0/1 を正常(スコア取得成功)、2 を実行エラーとして扱う。
package riskscan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Binary は呼び出す skillspector 実行ファイル名。PATH から解決する。
const Binary = "skillspector"

// Result は skillspector の risk_assessment を写した評価結果。
type Result struct {
	Score          int    // 0-100
	Severity       string // LOW / MEDIUM / HIGH / CRITICAL
	Recommendation string // SAFE / CAUTION / DO_NOT_INSTALL
}

// Available は skillspector が PATH 上にあるかを返す。
// 未導入の環境ではリスク再評価を静かにスキップするための判定に使う。
func Available() bool {
	_, err := exec.LookPath(Binary)
	return err == nil
}

// Scan は dir をスキャンしてリスク評価を返す。
func Scan(ctx context.Context, dir string) (Result, error) {
	cmd := exec.CommandContext(ctx, Binary, "scan", dir, "--format", "json", "--no-llm")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// exit 1 は「スコア>50」で正常。JSONは出ているので解析へ進む。
			if ee.ExitCode() != 1 {
				return Result{}, fmt.Errorf("skillspector scan %s が失敗した(exit %d): %s",
					dir, ee.ExitCode(), strings.TrimSpace(stderr.String()))
			}
		} else {
			return Result{}, fmt.Errorf("skillspector の起動に失敗した: %w", err)
		}
	}
	return parseResult(stdout.Bytes())
}

// ScanFiles は上流から取得したファイル群(キーは "/" 区切りの相対パス)を
// 一時ディレクトリへ展開してスキャンする。呼び出し後に一時ディレクトリは消す。
func ScanFiles(ctx context.Context, files map[string][]byte) (Result, error) {
	dir, err := os.MkdirTemp("", "skilldrift-scan-")
	if err != nil {
		return Result{}, fmt.Errorf("一時ディレクトリの作成に失敗した: %w", err)
	}
	defer os.RemoveAll(dir)

	for p, data := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return Result{}, err
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return Result{}, err
		}
	}
	return Scan(ctx, dir)
}

// parseResult は skillspector のJSON出力から risk_assessment を取り出す。
func parseResult(data []byte) (Result, error) {
	var raw struct {
		RiskAssessment struct {
			Score          int    `json:"score"`
			Severity       string `json:"severity"`
			Recommendation string `json:"recommendation"`
		} `json:"risk_assessment"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Result{}, fmt.Errorf("skillspector のJSON出力の解析に失敗した: %w", err)
	}
	if raw.RiskAssessment.Severity == "" && raw.RiskAssessment.Recommendation == "" {
		return Result{}, fmt.Errorf("skillspector のJSON出力に risk_assessment が無い")
	}
	return Result{
		Score:          raw.RiskAssessment.Score,
		Severity:       raw.RiskAssessment.Severity,
		Recommendation: raw.RiskAssessment.Recommendation,
	}, nil
}
