// Package manage 的模组清单测试: 形状、内容哈希一致性 (契约唯一信任
// 依据)、目录打包确定性、缓存重建、大小上限、冲突取舍、白名单.
package manage

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/CoreMatch/Avalon/internal/util/file"
)

// withModsDir 把数据根指到临时目录 (mods 落在 <tmp>/data/mods),
// 结束恢复 BasePath 并清清单缓存.
func withModsDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	oldBase := file.BasePath()
	file.SetBasePath(tmp)
	modsDir := filepath.Join(tmp, "data", "mods")
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		t.Fatalf("建 mods 目录: %v", err)
	}
	resetModManifestCache()
	t.Cleanup(func() {
		file.SetBasePath(oldBase)
		resetModManifestCache()
	})
	return modsDir
}

// entryByFile 在清单里按 file 找条目
func entryByFile(t *testing.T, m *ModManifest, fileName string) ModManifestEntry {
	t.Helper()
	for _, e := range m.Mods {
		if e.File == fileName {
			return e
		}
	}
	t.Fatalf("清单缺少条目 %s: %+v", fileName, m.Mods)
	return ModManifestEntry{}
}

func TestModManifestShapesAndHashes(t *testing.T) {
	modsDir := withModsDir(t)

	// zip 模组
	zipPath := filepath.Join(modsDir, "Alpha.zip")
	writeZip(t, zipPath, map[string]string{"units/alpha.ini": "alpha"})
	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	// 目录模组
	dirMod := filepath.Join(modsDir, "Beta")
	if err := os.MkdirAll(filepath.Join(dirMod, "units"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirMod, "units", "beta.ini"), []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 不支持/隐藏/非法名条目: 全部应跳过
	if err := os.WriteFile(filepath.Join(modsDir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modsDir, ".hidden.zip"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(modsDir, "bad name!"), 0o755); err != nil {
		t.Fatal(err)
	}

	m, err := BuildModManifest()
	if err != nil {
		t.Fatalf("BuildModManifest: %v", err)
	}
	if len(m.Mods) != 2 {
		t.Fatalf("期望恰 2 条 (Alpha.zip + Beta.zip), got %+v", m.Mods)
	}
	if m.Sig != nil || m.SigAlg != nil {
		t.Fatalf("v1.3 签名位必须为 nil: %v / %v", m.Sig, m.SigAlg)
	}

	alpha := entryByFile(t, m, "Alpha.zip")
	if alpha.Name != "Alpha" {
		t.Fatalf("Alpha name = %q", alpha.Name)
	}
	if alpha.Size != int64(len(zipBytes)) {
		t.Fatalf("Alpha size = %d, want %d", alpha.Size, len(zipBytes))
	}
	wantSum := sha256.Sum256(zipBytes)
	if alpha.SHA256 != hex.EncodeToString(wantSum[:]) {
		t.Fatalf("Alpha sha256 与文件字节不一致")
	}

	beta := entryByFile(t, m, "Beta.zip")
	if beta.Name != "Beta" {
		t.Fatalf("Beta name = %q", beta.Name)
	}

	// 契约核心不变量: 每个条目的 sha256/size 与可服务字节严格一致
	for _, e := range m.Mods {
		data, ok, err := ModArtifact(e.File)
		if err != nil || !ok {
			t.Fatalf("ModArtifact(%s): ok=%v err=%v", e.File, ok, err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != e.SHA256 || int64(len(data)) != e.Size {
			t.Fatalf("条目 %s 的哈希/大小与产物字节不一致", e.File)
		}
	}

	// 目录模组打包产物必须含原文件 (引擎挂载后解析内容一致的前提)
	betaData, _, _ := ModArtifact("Beta.zip")
	zr, err := zip.NewReader(bytes.NewReader(betaData), int64(len(betaData)))
	if err != nil {
		t.Fatalf("Beta.zip 不是合法 zip: %v", err)
	}
	found := false
	for _, zf := range zr.File {
		if zf.Name == "units/beta.ini" {
			rc, err := zf.Open()
			if err != nil {
				t.Fatal(err)
			}
			content, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "beta" {
				t.Fatalf("beta.ini 内容 = %q", content)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("Beta.zip 缺少 units/beta.ini")
	}
}

func TestModManifestEmpty(t *testing.T) {
	withModsDir(t)
	m, err := BuildModManifest()
	if err != nil {
		t.Fatalf("BuildModManifest: %v", err)
	}
	if m.Mods == nil || len(m.Mods) != 0 {
		t.Fatalf("空目录应返回空 (非 nil) 清单: %+v", m.Mods)
	}
}

func TestModManifestRebuildOnChange(t *testing.T) {
	modsDir := withModsDir(t)
	writeZip(t, filepath.Join(modsDir, "Alpha.zip"), map[string]string{"a.ini": "a"})

	m, err := BuildModManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Mods) != 1 {
		t.Fatalf("got %+v", m.Mods)
	}

	// 目录指纹变化 -> 重建
	writeZip(t, filepath.Join(modsDir, "Gamma.zip"), map[string]string{"g.ini": "g"})
	m, err = BuildModManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Mods) != 2 {
		t.Fatalf("新增模组后应 2 条: %+v", m.Mods)
	}
	if _, ok, err := ModArtifact("Gamma.zip"); err != nil || !ok {
		t.Fatalf("Gamma.zip 产物缺失: ok=%v err=%v", ok, err)
	}
}

func TestModManifestDirPackagingDeterministic(t *testing.T) {
	modsDir := withModsDir(t)
	dirMod := filepath.Join(modsDir, "Beta")
	if err := os.MkdirAll(filepath.Join(dirMod, "units"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"b1.ini", "b2.ini"} {
		if err := os.WriteFile(filepath.Join(dirMod, "units", name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m1, err := BuildModManifest()
	if err != nil {
		t.Fatal(err)
	}
	first := entryByFile(t, m1, "Beta.zip").SHA256

	resetModManifestCache()
	m2, err := BuildModManifest()
	if err != nil {
		t.Fatal(err)
	}
	if second := entryByFile(t, m2, "Beta.zip").SHA256; second != first {
		t.Fatalf("目录打包不确定: %s != %s", first, second)
	}
}

func TestModManifestSizeLimit(t *testing.T) {
	modsDir := withModsDir(t)
	oldLimit := maxModFileSize
	maxModFileSize = 10
	t.Cleanup(func() { maxModFileSize = oldLimit })

	writeZip(t, filepath.Join(modsDir, "Big.zip"), map[string]string{"big.ini": "0123456789abcdef"})
	m, err := BuildModManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Mods) != 0 {
		t.Fatalf("超限模组应被跳过: %+v", m.Mods)
	}
}

func TestModManifestZipFileWinsConflict(t *testing.T) {
	modsDir := withModsDir(t)
	// 目录 Gamma 与 Gamma.zip 并存: zip 文件优先
	if err := os.MkdirAll(filepath.Join(modsDir, "Gamma"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modsDir, "Gamma", "g.ini"), []byte("dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeZip(t, filepath.Join(modsDir, "Gamma.zip"), map[string]string{"g.ini": "zip"})
	zipBytes, err := os.ReadFile(filepath.Join(modsDir, "Gamma.zip"))
	if err != nil {
		t.Fatal(err)
	}

	m, err := BuildModManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Mods) != 1 {
		t.Fatalf("冲突应合并为一条: %+v", m.Mods)
	}
	data, ok, err := ModArtifact("Gamma.zip")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(data, zipBytes) {
		t.Fatal("冲突时应服务真正的 zip 文件字节")
	}
}

func TestValidModFileName(t *testing.T) {
	cases := map[string]bool{
		"Alpha.zip": true,
		"a-b_c.9":   true,
		".":         false,
		"..":        false,
		"a/b":       false,
		"a b":       false,
		"中文.zip":    false,
		"":          false,
	}
	for name, want := range cases {
		if got := ValidModFileName(name); got != want {
			t.Errorf("ValidModFileName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestModArtifactUnknown(t *testing.T) {
	withModsDir(t)
	if _, ok, err := ModArtifact("NoSuch.zip"); err != nil || ok {
		t.Fatalf("未收录文件应 !ok: ok=%v err=%v", ok, err)
	}
}
