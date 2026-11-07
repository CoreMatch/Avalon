package realize

// 插件建房准入在 relay 路径的接线测试 (ADR-0006).

import (
	"bytes"
	"testing"

	"github.com/CoreMatch/Avalon/internal/io/packettype"
	"github.com/CoreMatch/Avalon/internal/plugin/plugintest"
	"github.com/CoreMatch/Avalon/internal/util/file"
)

// TestRelayRoomCreationDeniedByPlugin 插件否决: 不建房, 理由经 RELAY_117
// 错误信道回到请求方
func TestRelayRoomCreationDeniedByPlugin(t *testing.T) {
	oldBase := file.BasePath()
	file.SetBasePath(t.TempDir())
	t.Cleanup(func() { file.SetBasePath(oldBase) })

	srv := plugintest.Start(t, true, "维护中, 稍后再来")
	plugintest.Install(t, srv.Entry("gate"))

	g, w := newTestRelay(t, "127.0.0.1")
	g.name = "alice"
	g.registerPlayerId = "aabbcc"

	g.idCustom("new")

	if g.room != nil {
		g.room.Re()
		t.Fatal("插件否决后不应创建房间")
	}
	if w.countType(packettype.RELAY_117) != 1 {
		t.Fatalf("应回送 RELAY_117 错误提示, got %d", w.countType(packettype.RELAY_117))
	}
	pkt := w.lastOfType(packettype.RELAY_117)
	if !bytes.Contains(pkt.Bytes, []byte("维护中, 稍后再来")) {
		t.Fatalf("错误提示应包含否决理由, got %q", pkt.Bytes)
	}
}

// TestRelayRoomCreationAllowedByPlugin 插件放行: 建房不受影响 (接线零回归)
func TestRelayRoomCreationAllowedByPlugin(t *testing.T) {
	oldBase := file.BasePath()
	file.SetBasePath(t.TempDir())
	t.Cleanup(func() { file.SetBasePath(oldBase) })

	srv := plugintest.Start(t, false, "")
	plugintest.Install(t, srv.Entry("gate"))

	g, _ := newTestRelay(t, "127.0.0.1")
	g.name = "alice"
	g.registerPlayerId = "aabbcc"

	g.idCustom("new")

	if g.room == nil {
		t.Fatal("插件放行时应正常建房")
	}
	g.room.Re()
	if srv.Requests() == 0 {
		t.Fatal("准入调用应到达插件")
	}
}
