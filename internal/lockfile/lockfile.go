// Package lockfile は skilldrift.lock の読み書きを提供する。
// lockfileが真の情報源であり、スキーマ変更は後方互換を壊さないこと
// (versionフィールドで世代管理し、将来MCP設定・pluginのセクション追加に備える)。
//
// ドリフト検知の粒度はハイブリッド方式:
//   - 最終判定はコンテンツハッシュ(Files)。上流repoのスキル外変更による
//     誤検知を避け、commitが動かないすり替え(force-push等)も捕捉する
//   - Source.Commit は check の短絡判定(一致なら取得省略)と
//     drift時のdiff起点として記録する
//   - SKILL.md以外の同梱ファイル(スクリプト等)も全て対象
//
// skilldrift.lock の例:
//
//	{
//	  "version": 1,
//	  "skills": [
//	    {
//	      "name": "deep-research",
//	      "path": "skills/deep-research",
//	      "source": {
//	        "type": "github",
//	        "repo": "owner/skills-repo",
//	        "ref": "main",
//	        "subdir": "skills/deep-research",
//	        "commit": "0123abcd..."
//	      },
//	      "files": {
//	        "SKILL.md": "sha256:9f86d08...",
//	        "scripts/fetch.sh": "sha256:2cf24db..."
//	      }
//	    }
//	  ]
//	}
package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

const (
	// DefaultPath はリポジトリルートに置くlockfileの既定ファイル名。
	DefaultPath = "skilldrift.lock"

	// SchemaVersion はこのバイナリが書き出すスキーマバージョン。
	// 後方互換を壊す変更をしない限り上げない。
	SchemaVersion = 1

	// SourceTypeGitHub は現状唯一の出自種別。
	SourceTypeGitHub = "github"
)

// Lockfile は skilldrift.lock 全体を表す。
type Lockfile struct {
	Version int     `json:"version"`
	Skills  []Skill `json:"skills"`
}

// Skill は導入済みスキル1つの出自とコンテンツの記録。
type Skill struct {
	// Name はスキル名(スキルディレクトリ名)。lockfile内で一意。
	Name string `json:"name"`
	// Path はリポジトリルートからスキルディレクトリへの相対パス。
	Path string `json:"path"`
	// Source は上流の出自。initは空欄のテンプレートを書き出し、手で埋める。
	Source Source `json:"source"`
	// Files はスキルディレクトリ配下の全ファイルのコンテンツハッシュ。
	// キーはスキルディレクトリからの相対パス(区切りは常に "/")、
	// 値は HashBytes 形式("sha256:<hex>")。ドリフトの最終判定はここで行う。
	Files map[string]string `json:"files"`
}

// Source はスキルの上流出自。
type Source struct {
	// Type は出自の種別。現状 SourceTypeGitHub のみ。
	Type string `json:"type"`
	// Repo は "owner/name" 形式。空なら出自未記入(監視対象外)。
	Repo string `json:"repo"`
	// Ref は追跡するブランチまたはタグ。空なら上流のデフォルトブランチ。
	Ref string `json:"ref,omitempty"`
	// Subdir は上流リポジトリ内でスキルが置かれているディレクトリ。空ならルート直下。
	Subdir string `json:"subdir,omitempty"`
	// Commit は最後に確認した上流のcommit SHA。
	Commit string `json:"commit"`
}

// Tracked は出自が記入済みで監視対象になっているかを返す。
func (s Skill) Tracked() bool { return s.Source.Repo != "" }

// New は現行スキーマバージョンの空のLockfileを返す。
func New() *Lockfile {
	return &Lockfile{Version: SchemaVersion}
}

// Load は path のlockfileを読み込み、スキーマを検証して返す。
// ファイルが存在しない場合は fs.ErrNotExist を包んだエラーを返す。
func Load(path string) (*Lockfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lf Lockfile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("%s の解析に失敗: %w", path, err)
	}
	if err := lf.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &lf, nil
}

// Save は lockfile を path に書き出す。出力は決定的
// (スキルはname昇順、ファイルはキー昇順、2スペースインデント、末尾改行)で、
// git上のdiffが変更内容だけになるようにする。
func (lf *Lockfile) Save(path string) error {
	if err := lf.validate(); err != nil {
		return err
	}
	out := Lockfile{Version: lf.Version, Skills: append([]Skill(nil), lf.Skills...)}
	if out.Version == 0 {
		out.Version = SchemaVersion
	}
	sort.Slice(out.Skills, func(i, j int) bool { return out.Skills[i].Name < out.Skills[j].Name })
	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return fmt.Errorf("lockfileのシリアライズに失敗: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (lf *Lockfile) validate() error {
	if lf.Version > SchemaVersion {
		return fmt.Errorf("schema version %d はこのバイナリの対応上限(%d)より新しい。skilldriftを更新してください", lf.Version, SchemaVersion)
	}
	seen := make(map[string]bool, len(lf.Skills))
	for i, s := range lf.Skills {
		if s.Name == "" {
			return fmt.Errorf("skills[%d]: name が空", i)
		}
		if s.Path == "" {
			return fmt.Errorf("スキル %q: path が空", s.Name)
		}
		if seen[s.Name] {
			return fmt.Errorf("スキル %q が重複している", s.Name)
		}
		seen[s.Name] = true
	}
	return nil
}

// Changes は Reconcile によるlockfileの変化の要約。
type Changes struct {
	Added   []string // 新規に追加したスキル名
	Removed []string // ローカルから消えたため削除したスキル名
}

// Reconcile はローカル走査結果 found を現在の承認済みベースラインとして
// lockfileに反映する。既存エントリの Source(手で記入した出自)は保持し、
// Path と Files は現状で上書きする。found に無い既存エントリは削除する。
func (lf *Lockfile) Reconcile(found []Skill) Changes {
	existing := make(map[string]Skill, len(lf.Skills))
	for _, s := range lf.Skills {
		existing[s.Name] = s
	}
	var ch Changes
	foundNames := make(map[string]bool, len(found))
	skills := make([]Skill, 0, len(found))
	for _, f := range found {
		foundNames[f.Name] = true
		if prev, ok := existing[f.Name]; ok {
			f.Source = prev.Source
		} else {
			ch.Added = append(ch.Added, f.Name)
		}
		skills = append(skills, f)
	}
	for _, s := range lf.Skills {
		if !foundNames[s.Name] {
			ch.Removed = append(ch.Removed, s.Name)
		}
	}
	lf.Skills = skills
	return ch
}

// HashBytes は b のコンテンツハッシュ表現("sha256:<hex>")を返す。
// Skill.Files の値はこの形式で記録する。
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// HashFile は path のファイル内容のコンテンツハッシュを返す。
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("%s の読み込みに失敗: %w", path, err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
