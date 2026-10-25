package net

import (
	"testing"

	"rwhps/internal/net/core"
	"rwhps/internal/util/game/command"
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

	_ = NewAbstractNet(core.NewNullRwHps(core.ServerProtocol))

	if core.LogCommand != logHandler {
		t.Error("NewAbstractNet cleared LogCommand")
	}
	if core.PingCommand != pingHandler {
		t.Error("NewAbstractNet cleared PingCommand")
	}
}
