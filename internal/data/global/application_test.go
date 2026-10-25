package global

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"rwhps/internal/util/file"
)

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// TestNewApplicationIdentity 校验身份生成规则与持久化
func TestNewApplicationIdentity(t *testing.T) {
	base := t.TempDir()
	file.SetBasePath(base)
	t.Cleanup(func() { file.SetBasePath(base) })

	a := NewApplication()
	if !uuidV4.MatchString(a.ServerConnectUUID) {
		t.Errorf("serverConnectUuid = %q, want uuid v4", a.ServerConnectUUID)
	}
	if !regexp.MustCompile(`^[0-9A-F]{1,64}$`).MatchString(a.ServerHessUUID) {
		t.Errorf("serverHessUuid = %q, want uppercase hex", a.ServerHessUUID)
	}
	if len(a.ServerToken) != 40 {
		t.Errorf("serverToken len = %d, want 40", len(a.ServerToken))
	}
	if a.Admin == nil || a.HTTP == nil || a.RWHTTP == nil {
		t.Error("application components not initialised")
	}

	if _, err := os.Stat(filepath.Join(base, "data", "Settings.bin")); err == nil {
		t.Log("settings file already exists before save")
	}

	a.Save()
	if _, err := os.Stat(filepath.Join(base, "data", "Settings.bin")); err != nil {
		t.Fatalf("Settings.bin not written: %v", err)
	}

	// 再次启动必须复用同一身份
	b := NewApplication()
	if b.ServerConnectUUID != a.ServerConnectUUID {
		t.Errorf("uuid changed across restart: %q != %q", b.ServerConnectUUID, a.ServerConnectUUID)
	}
	if b.ServerHessUUID != a.ServerHessUUID {
		t.Errorf("hess uuid changed across restart: %q != %q", b.ServerHessUUID, a.ServerHessUUID)
	}
	// token 每次启动都重新生成
	if b.ServerToken == a.ServerToken {
		t.Error("serverToken should be regenerated on each start")
	}
}

// TestAdministration 基础管理员/封禁操作
func TestAdministration(t *testing.T) {
	a := NewAdministration()
	a.AddAdmin("uuid-1", "alice", false)
	if !a.IsAdmin("uuid-1", "alice") {
		t.Error("alice should be admin")
	}
	if a.IsAdmin("uuid-2", "bob") {
		t.Error("bob should not be admin")
	}
	a.RemoveAdmin("uuid-1")
	if a.IsAdmin("uuid-1", "alice") {
		t.Error("admin should be removed")
	}

	a.BanIP24("1.2.3")
	a.BanUUID("uuid-3")
	if !a.IsIP24Banned("1.2.3") || !a.IsUUIDBanned("uuid-3") {
		t.Error("bans not applied")
	}
	a.ClearBanAll()
	if a.IsIP24Banned("1.2.3") || a.IsUUIDBanned("uuid-3") {
		t.Error("bans should be cleared")
	}
}

// TestStatisticians 计时器
func TestStatisticians(t *testing.T) {
	AddTime("unit.test")
	if got := ComputeTime("unit.test"); got < 0 {
		t.Errorf("computeTime = %d", got)
	}
	// 取走后即移除
	if got := ComputeTime("unit.test"); got != 0 {
		t.Errorf("second computeTime = %d, want 0", got)
	}
}
