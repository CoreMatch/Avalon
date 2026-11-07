// Package manage 的地图登记测试: 顺序表、zip 地图、不安全名字拦截.
package manage

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/CoreMatch/Avalon/internal/game"
	"github.com/CoreMatch/Avalon/internal/util/file"
)

// withMapDirs 把数据根指到临时目录 (maps 落在 <tmp>/data/maps), 结束恢复.
//
// 路径常量是 const, 只能经 file.SetBasePath 重定向.
func withMapDirs(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	oldBase := file.BasePath()
	file.SetBasePath(tmp)
	mapsDir := filepath.Join(tmp, "data", "maps")
	if err := os.MkdirAll(mapsDir, 0o755); err != nil {
		t.Fatalf("建 maps 目录: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "data", "saves"), 0o755); err != nil {
		t.Fatalf("建 saves 目录: %v", err)
	}
	t.Cleanup(func() {
		file.SetBasePath(oldBase)
		MapsData = map[string]*game.MapData{}
		mapsDataOrder = nil
	})
	return mapsDir
}

// writeZip 在 path 建一个含指定条目 (名字 -> 内容) 的 zip.
func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("建 zip: %v", err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip 条目 %q: %v", name, err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("写 zip 条目 %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 zip: %v", err)
	}
}

func TestValidMapName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"MyMap", true},
		{"map with space", true},
		{"", false},
		{".", false},
		{"..", false},
		{"..evil", false},
		{"a..b", false},
		{"sub/evil", false},
		{`sub\evil`, false},
	}
	for _, c := range cases {
		if got := validMapName(c.name); got != c.want {
			t.Errorf("validMapName(%q) = %v, 期望 %v", c.name, got, c.want)
		}
	}
}

func TestReadMapAndSaveRegistersZipAndSkipsUnsafe(t *testing.T) {
	mapsDir := withMapDirs(t)

	// 两张裸 tmx + 一个含合法与非法条目的 zip + 一个非法名字的裸文件
	for _, n := range []string{"beta.tmx", "alpha.tmx", "..bad.tmx"} {
		if err := os.WriteFile(filepath.Join(mapsDir, n), []byte("<map/>"), 0o644); err != nil {
			t.Fatalf("写 %q: %v", n, err)
		}
	}
	writeZip(t, filepath.Join(mapsDir, "pack.zip"), map[string]string{
		"inner.tmx":     "<map/>",
		"sub/evil.tmx":  "<map/>", // zip 内带路径的条目名必须被拒
		"../escape.tmx": "<map/>", // zip 内穿越条目名必须被拒
	})

	ReadMapAndSave()

	if MapDataCount() != 3 {
		t.Fatalf("应登记 3 张地图 (beta/alpha/inner), 实际 %d: %v", MapDataCount(), MapsData)
	}
	for _, banned := range []string{"..bad", "sub/evil", "../escape", "pack"} {
		if _, ok := MapsData[banned]; ok {
			t.Errorf("不安全/不应登记的名字 %q 进入了注册表", banned)
		}
	}

	// 顺序表与注册表一致, 且按序号可稳定取回
	seen := map[string]bool{}
	for i := 0; i < MapDataCount(); i++ {
		name, data, ok := MapDataByIndex(i)
		if !ok || data == nil {
			t.Fatalf("MapDataByIndex(%d) 取不到", i)
		}
		seen[name] = true
		name2, _, _ := MapDataByIndex(i)
		if name2 != name {
			t.Fatalf("MapDataByIndex(%d) 两次结果不一致: %q vs %q", i, name, name2)
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	want := []string{"alpha", "beta", "inner"}
	if len(names) != len(want) {
		t.Fatalf("登记集合 = %v, 期望 %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("登记集合 = %v, 期望 %v", names, want)
		}
	}

	// 重扫幂等: 不重复登记
	ReadMapAndSave()
	if MapDataCount() != 3 {
		t.Fatalf("重扫后应仍为 3 张, 实际 %d", MapDataCount())
	}
}

func TestMapDataByIndexOutOfRange(t *testing.T) {
	withMapDirs(t)
	ReadMapAndSave()

	if _, _, ok := MapDataByIndex(0); ok {
		t.Fatal("空注册表按序号 0 取应为 false")
	}
	if _, _, ok := MapDataByIndex(-1); ok {
		t.Fatal("负序号应返回 false")
	}
}

// TestReadMapAndSaveRegistersManifest 协议 v4: 登记即生成内容清单
// (SHA-256 + 大小), File 形态取盘上字节, Zip 形态取将被物化的条目字节.
func TestReadMapAndSaveRegistersManifest(t *testing.T) {
	mapsDir := withMapDirs(t)

	if err := os.WriteFile(filepath.Join(mapsDir, "plain.tmx"), []byte("<map/>"), 0o644); err != nil {
		t.Fatalf("写地图: %v", err)
	}
	writeZip(t, filepath.Join(mapsDir, "pack.zip"), map[string]string{"inner.tmx": "<map/>"})

	ReadMapAndSave()

	const wantSHA = "0e9a897681da892fbb62033be399b94032d5eb71d47251bbff67b651747dbc42"
	for _, name := range []string{"plain", "inner"} {
		data, ok := MapsData[name]
		if !ok {
			t.Fatalf("地图 %q 未登记", name)
		}
		if data.ContentSHA256() != wantSHA || data.ContentSize() != int64(len("<map/>")) {
			t.Errorf("%q 清单 = %q/%d, 期望 %q/%d", name, data.ContentSHA256(), data.ContentSize(), wantSHA, len("<map/>"))
		}
	}

	// BuildMapManifest / LookupManifest 与注册表同源
	m := BuildMapManifest()
	if len(m.Maps) != 2 {
		t.Fatalf("清单条目数 = %d, 期望 2", len(m.Maps))
	}
	entry, ok := LookupManifest("plain")
	if !ok || entry.SHA256 != wantSHA || entry.Size != 6 || entry.File != "plain.tmx" {
		t.Fatalf("LookupManifest(plain) = %+v, ok=%v", entry, ok)
	}
	if _, ok := LookupManifest("ghost"); ok {
		t.Fatal("未登记地图不应出现在清单里")
	}
}
