package gateway

// 插件建房准入在 Cascade 控制面的接线测试 (ADR-0006).

import (
	"net/http"
	"testing"

	"github.com/CoreMatch/Avalon/internal/plugin/plugintest"
	"github.com/CoreMatch/Avalon/internal/util/file"
)

// TestControlCreateRoomDeniedByPlugin 插件否决: 403 FORBIDDEN + 理由进 message
func TestControlCreateRoomDeniedByPlugin(t *testing.T) {
	oldBase := file.BasePath()
	file.SetBasePath(t.TempDir())
	t.Cleanup(func() { file.SetBasePath(oldBase) })

	srv := plugintest.Start(t, true, "maintenance window")
	plugintest.Install(t, srv.Entry("gate"))

	_, base, client, _ := controlFixture(t)
	withTestRoom(t)

	resp, body := doJSON(t, client, http.MethodPost, base+"/rooms/create",
		map[string]any{"code": "GW01", "maxPlayers": 8})
	if resp.StatusCode != http.StatusForbidden || body["code"] != CodeForbidden {
		t.Fatalf("否决应 403 FORBIDDEN, got %d %v", resp.StatusCode, body)
	}
	if body["message"] != "gate: maintenance window" {
		t.Fatalf("message 应为 插件名: 理由, got %v", body["message"])
	}
}

// TestControlCreateRoomAllowedByPlugin 插件放行: 既有 201 幂等语义不变
func TestControlCreateRoomAllowedByPlugin(t *testing.T) {
	oldBase := file.BasePath()
	file.SetBasePath(t.TempDir())
	t.Cleanup(func() { file.SetBasePath(oldBase) })

	srv := plugintest.Start(t, false, "")
	plugintest.Install(t, srv.Entry("gate"))

	_, base, client, _ := controlFixture(t)
	withTestRoom(t)

	resp, body := doJSON(t, client, http.MethodPost, base+"/rooms/create",
		map[string]any{"code": "GW01", "maxPlayers": 8})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("放行应 201, got %d %v", resp.StatusCode, body)
	}
	if srv.Requests() == 0 {
		t.Fatal("准入调用应到达插件")
	}
}
