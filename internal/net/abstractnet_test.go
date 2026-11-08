package net

import (
	"net"
	"testing"

	"github.com/CoreMatch/Avalon/internal/net/core"
	"github.com/CoreMatch/Avalon/internal/util/game/command"
)

// TestNewAbstractNetKeepsCommandHooks 构造监听器不得清空已注入的命令钩子,
// 否则 Debug/Ping/日志命令会静默失效 (对应 core.LogCommand/PingCommand).
func TestNewAbstractNetKeepsCommandHooks(t *testing.T) {
	logHandler := command.NewHandler("!")
	pingHandler := command.NewHandler("")
	core.LogCommand = logHandler
	core.PingCommand = pingHandler
	t.Cleanup(func() {
		core.LogCommand = nil
		core.PingCommand = nil
	})

	an := NewAbstractNet(core.NewNullProtocolProvider(core.ServerProtocol))
	an.Close()

	if core.LogCommand != logHandler {
		t.Error("NewAbstractNet cleared LogCommand")
	}
	if core.PingCommand != pingHandler {
		t.Error("NewAbstractNet cleared PingCommand")
	}
}

// TestTimeoutDetection 超时判定: 与当前时间比空闲时长, 而非两个"最后收到"互比
//
// 回归: 旧实现拿 ch.LastReceiveMs() 当"当前时间", 语义错误且零调用,
// 半开连接永不回收 (玩家幽灵). 现语义: 从未收到数据的连接判超时,
// 刚收到数据的不判.
func TestTimeoutDetection(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })

	ch := core.NewChannel(a)
	// lastRecv=0 (从未收到数据): 空闲无限长, 应判超时
	if !TimeoutDetection(ch) {
		t.Fatal("从未收到数据的连接应判超时")
	}
	// 刚收到数据: 不应判超时
	ch.RefreshLastAck()
	if TimeoutDetection(ch) {
		t.Fatal("刚收到数据的连接不应判超时")
	}
}
