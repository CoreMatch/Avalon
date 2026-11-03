package netconnectprotocol

import (
	"rwhps/internal/io/packet"
	"rwhps/internal/net/netconnectprotocol/internal/server"
)

// 本文件是 internal/server 的对外桥, 与 relayflavor.go 同理:
// Go 的 internal 包规则只允许 netconnectprotocol 子树内的代码直接导入
// internal/server; 服务端协议实现体 (net/gameserver) 需要其中的包构造器.

// PlayerExitPacket 对应 playerExitInternalPacket(cause): 玩家退出通知 (DISCONNECT 包)
func PlayerExitPacket(cause string) *packet.Packet { return server.PlayerExitPacket(cause) }
