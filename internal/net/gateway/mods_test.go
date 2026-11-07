package gateway

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/CoreMatch/Avalon/internal/util/file"
)

// 契约 v1.3 模组清单/文件端点的控制面测试 (错误形状与信任矩阵见
// control_matrix_test.go).

// withModsBase 把数据根指到临时目录并放一个 zip 模组, 返回模组文件名与
// 原始字节; 结束恢复 BasePath. 清单缓存按目录指纹自愈, 无需跨包重置.
func withModsBase(t *testing.T) (string, []byte) {
	t.Helper()
	tmp := t.TempDir()
	oldBase := file.BasePath()
	file.SetBasePath(tmp)
	t.Cleanup(func() { file.SetBasePath(oldBase) })

	modsDir := filepath.Join(tmp, "data", "mods")
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(modsDir, "Alpha.zip")
	writeZipFile(t, zipPath, map[string]string{"units/alpha.ini": "alpha"})
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	return "Alpha.zip", data
}

// writeZipFile 在 path 建一个含指定条目的 zip (测试夹具)
func writeZipFile(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestControlRoomModsManifest(t *testing.T) {
	_, base, client, _ := controlFixture(t)
	withModsBase(t)

	resp, body := doJSON(t, client, http.MethodGet, base+"/rooms/GW01/mods", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, body=%v", resp.StatusCode, body)
	}
	mods, ok := body["mods"].([]any)
	if !ok || len(mods) != 1 {
		t.Fatalf("mods = %v (期望恰一条)", body["mods"])
	}
	one := mods[0].(map[string]any)
	for _, key := range []string{"name", "file", "sha256", "size"} {
		if _, has := one[key]; !has {
			t.Fatalf("清单条目缺字段 %s: %v", key, one)
		}
	}
	if one["name"] != "Alpha" || one["file"] != "Alpha.zip" {
		t.Fatalf("条目 = %v", one)
	}
	if sig, has := body["sig"]; has && sig != nil {
		t.Fatalf("v1.3 sig 必须为 null: %v", sig)
	}
	if sigAlg, has := body["sigAlg"]; has && sigAlg != nil {
		t.Fatalf("v1.3 sigAlg 必须为 null: %v", sigAlg)
	}
}

func TestControlRoomModsEmptyManifest(t *testing.T) {
	_, base, client, _ := controlFixture(t)
	// 空 mods 目录 (零模组房间): 契约合法形态, 200 + 空数组
	tmp := t.TempDir()
	oldBase := file.BasePath()
	file.SetBasePath(tmp)
	t.Cleanup(func() { file.SetBasePath(oldBase) })

	resp, body := doJSON(t, client, http.MethodGet, base+"/rooms/GW01/mods", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, body=%v", resp.StatusCode, body)
	}
	mods, ok := body["mods"].([]any)
	if !ok || len(mods) != 0 {
		t.Fatalf("零模组房间应为空数组: %v", body["mods"])
	}
}

func TestControlRoomModFileServesManifestBytes(t *testing.T) {
	_, base, client, _ := controlFixture(t)
	zipName, zipBytes := withModsBase(t)

	req, err := http.NewRequest(http.MethodGet, base+"/rooms/GW01/mods/"+zipName, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(zipBytes) {
		t.Fatalf("字节数 = %d, want %d", len(data), len(zipBytes))
	}
	sum := sha256.Sum256(data)
	wantSum := sha256.Sum256(zipBytes)
	if hex.EncodeToString(sum[:]) != hex.EncodeToString(wantSum[:]) {
		t.Fatal("内容端点字节与清单哈希不一致")
	}
}

func TestControlRoomModsErrorShapes(t *testing.T) {
	_, base, client, _ := controlFixture(t)
	withModsBase(t)

	// 非本后端房间码 -> 404 ROOM_NOT_FOUND
	resp, body := doJSON(t, client, http.MethodGet, base+"/rooms/OTHER/mods", nil)
	if resp.StatusCode != http.StatusNotFound || body["code"] != CodeRoomNotFound {
		t.Fatalf("got %d / %v", resp.StatusCode, body)
	}

	// 清单未收录的文件 -> 404 ROOM_NOT_FOUND
	resp, body = doJSON(t, client, http.MethodGet, base+"/rooms/GW01/mods/NoSuch.zip", nil)
	if resp.StatusCode != http.StatusNotFound || body["code"] != CodeRoomNotFound {
		t.Fatalf("got %d / %v", resp.StatusCode, body)
	}

	// 白名单外文件名 -> 400 INVALID_REQUEST (URL 编码的空格)
	req, err := http.NewRequest(http.MethodGet, base+"/rooms/GW01/mods/bad%20name.zip", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("白名单外文件名应 400, got %d", resp2.StatusCode)
	}
}
