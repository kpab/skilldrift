package riskscan

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseResult(t *testing.T) {
	const in = `{
		"skill": {"name": "demo"},
		"risk_assessment": {"score": 42, "severity": "MEDIUM", "recommendation": "CAUTION"},
		"issues": []
	}`
	got, err := parseResult([]byte(in))
	if err != nil {
		t.Fatalf("parseResult: %v", err)
	}
	want := Result{Score: 42, Severity: "MEDIUM", Recommendation: "CAUTION"}
	if got != want {
		t.Fatalf("parseResult = %+v, want %+v", got, want)
	}
}

func TestParseResultErrors(t *testing.T) {
	cases := map[string]string{
		"不正なJSON":           `{`,
		"risk_assessmentなし": `{"skill": {"name": "x"}}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseResult([]byte(in)); err == nil {
				t.Fatalf("エラーを期待したが nil")
			}
		})
	}
}

// stubSkillspector は skillspector を模したスクリプトをPATHの先頭に置く。
// 与えたJSONを標準出力に出し、exit code で終わる。
func stubSkillspector(t *testing.T, stdout string, exitCode int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stubはPOSIXシェル前提")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\ncat <<'EOF'\n" + stdout + "\nEOF\nexit " + itoa(exitCode) + "\n"
	path := filepath.Join(dir, Binary)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestScanExitZeroAndOne(t *testing.T) {
	// exit 0(score≤50)も exit 1(score>50)も正常にJSONを解析できること。
	for _, tc := range []struct {
		name string
		code int
		json string
		want Result
	}{
		{"低リスク_exit0", 0, `{"risk_assessment":{"score":5,"severity":"LOW","recommendation":"SAFE"}}`, Result{5, "LOW", "SAFE"}},
		{"高リスク_exit1", 1, `{"risk_assessment":{"score":80,"severity":"HIGH","recommendation":"DO_NOT_INSTALL"}}`, Result{80, "HIGH", "DO_NOT_INSTALL"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubSkillspector(t, tc.json, tc.code)
			got, err := Scan(context.Background(), t.TempDir())
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Scan = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestScanExitTwoIsError(t *testing.T) {
	stubSkillspector(t, "", 2)
	if _, err := Scan(context.Background(), t.TempDir()); err == nil {
		t.Fatal("exit 2 でエラーを期待したが nil")
	}
}

func TestScanFiles(t *testing.T) {
	stubSkillspector(t, `{"risk_assessment":{"score":10,"severity":"LOW","recommendation":"SAFE"}}`, 0)
	files := map[string][]byte{
		"SKILL.md":       []byte("# demo\n"),
		"scripts/run.sh": []byte("echo hi\n"),
	}
	got, err := ScanFiles(context.Background(), files)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	if got.Score != 10 {
		t.Fatalf("ScanFiles score = %d, want 10", got.Score)
	}
}
