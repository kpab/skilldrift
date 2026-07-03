// Package scan はリポジトリを走査してスキルディレクトリを発見し、
// lockfileに記録する形(コンテンツハッシュ付き)のエントリを作る。
package scan

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/kpab/skilldrift/internal/lockfile"
)

// Skills は root 以下を走査し、SKILL.md を含むディレクトリをスキルとして返す。
// 返り値の Source は出自未記入のテンプレート(typeのみ)で、手で埋める前提。
// スキルディレクトリの内側はそのスキルの一部として扱い、別スキルとしては走査しない。
// root直下のSKILL.mdは、配下にスキルが1つも無い場合のみ「リポジトリ全体が
// 単一スキル」として扱う(一覧用にルートへSKILL.mdを置くリポジトリがあるため)。
func Skills(root string) ([]lockfile.Skill, error) {
	var skills []lockfile.Skill
	seen := map[string]string{} // name -> path(重複検出用)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || path == root {
			return nil
		}
		if d.Name() == ".git" || d.Name() == "node_modules" {
			return fs.SkipDir
		}
		if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
			return nil // SKILL.mdが無いディレクトリは走査を続ける
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := d.Name()
		if prev, dup := seen[name]; dup {
			return fmt.Errorf("スキル名 %q が重複している(%s と %s)。ディレクトリ名はリポジトリ内で一意にしてください", name, prev, rel)
		}
		seen[name] = rel

		files, err := hashDir(path)
		if err != nil {
			return err
		}
		skills = append(skills, lockfile.Skill{
			Name:   name,
			Path:   filepath.ToSlash(rel),
			Source: lockfile.Source{Type: lockfile.SourceTypeGitHub},
			Files:  files,
		})
		return fs.SkipDir
	})
	if err != nil {
		return nil, err
	}

	if len(skills) == 0 {
		if _, err := os.Stat(filepath.Join(root, "SKILL.md")); err == nil {
			abs, err := filepath.Abs(root)
			if err != nil {
				return nil, err
			}
			files, err := hashDir(root)
			if err != nil {
				return nil, err
			}
			skills = append(skills, lockfile.Skill{
				Name:   filepath.Base(abs),
				Path:   ".",
				Source: lockfile.Source{Type: lockfile.SourceTypeGitHub},
				Files:  files,
			})
		}
	}
	return skills, nil
}

// hashDir は dir 配下の全通常ファイルのコンテンツハッシュを集める。
// キーは dir からの相対パス(区切りは常に "/")。symlink等の非通常ファイルは対象外。
func hashDir(dir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		h, err := lockfile.HashFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = h
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
