// Package command 对应 net.rwhps.server.command.CommandsEx
package command

import (
	"rwhps/internal/data/global"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net"
	"rwhps/internal/net/core"
	cmd "rwhps/internal/util/game/command"
)

// registerPingCommands 对应 CommandsEx.registerCore
//
// hi 注册在 PING_COMMAND 上 (对应 GET_SERVER_INFO_RECEIVE), 由
// AbstractNetConnect.ExCommand 调用, 参数是发起查询的那条连接.
func registerPingCommands(h *cmd.Handler, d *Deps) {
	h.Register("hi", "", func(_ []string, param any) {
		con, _ := param.(*core.AbstractNetConnect)
		if con == nil {
			return
		}

		out := gameio.NewGameOutputStream()
		rw := net.NetStaticData.RwHps()

		if rw.NetType() == core.NullProtocol {
			out.WriteString("SERVER CLOSE")
		} else {
			out.WriteString("ServerNetType")
			out.WriteString(rw.NetType().String())
		}

		switch {
		case core.CheckProtocolIsServer(rw.NetType()):
			out.WriteString("PlayerSize")
			if d.Session != nil {
				out.WriteInt(int32(d.Session.PlayerCount()))
			} else {
				out.WriteInt(0)
			}
			out.WriteString("MaxPlayer")
			out.WriteInt(int32(global.ConfigServer.MaxPlayer))
			out.WriteString("MapName")
			if d.Session != nil {
				out.WriteString(d.Session.Snapshot().MapName)
			}

			// 原版此处还会继续写 Income / Credits / NoNukes / InitUnit / Mist /
			// SharedControl, 这六项全部来自 gameLinkServerData, 也就是游戏内核的
			// 服务端设置. Go 版没有内核, 也就不存在这些值 —— 与其填 0 假装, 不如不写.
			// 客户端按"读到缓冲区结束"解析键值对, 因此少写几项不会破坏格式.

		case rw.NetType() == core.RelayProtocol || rw.NetType() == core.RelayMulticastProtocol:
			total := 0
			for _, s := range net.NetStaticData.Services() {
				total += s.GetConnectSize()
			}
			out.WriteString("PlayerSize")
			out.WriteInt(int32(total))

			// RoomAllSize / RoomPublicListSize / RoomNoStartSize 来自 RelayRoom,
			// 属于 Relay 批次, 同样先不写.

		default:
			// HttpProtocol / RemoteControlProtocol / GameProtocol 等原版也是空分支
		}

		con.SendPacket(out.CreatePacket(packettype.GET_SERVER_INFO))
	})
}
