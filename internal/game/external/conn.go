package external

import (
	"rwhps/internal/io/packet"
)

// ConnOpen 向 helper 登记一条新的客户端连接 (对应 conn.open)
//
// 时机与上游一致: 上游在 getTypeConnect 里就把 PlayerConnectX 加进
// netEngine.aM (引擎从连接建立那一刻就知道它的存在), Go 侧同样在连接
// 处理器构造时登记, 而不是等到第一个游戏包.
func (b *Backend) ConnOpen(room, conn string) error {
	_, err := b.Request(&Request{Op: OpConnOpen, Room: room, Conn: conn})
	return err
}

// ConnClose 关闭一条客户端连接 (对应 conn.close)
func (b *Backend) ConnClose(room, conn string) error {
	_, err := b.Request(&Request{Op: OpConnClose, Room: room, Conn: conn})
	return err
}

// ConnPacket 把客户端发来的游戏包转给游戏引擎 (对应 conn.packet)
//
// fire-and-forget: 与上游 netEnginePackaging.addPacket 一样不等引擎处理结果,
// 但写失败必须报错 (调用方据此记日志), 否则客户端的包会静默丢失.
func (b *Backend) ConnPacket(room, conn string, p *packet.Packet) error {
	return b.Send(&Request{
		Op:    OpConnPacket,
		Room:  room,
		Conn:  conn,
		PType: p.Type.TypeInt(),
		Data:  encodeBase64(p.Bytes),
	})
}

// DecodePacketData 解码主动消息里的游戏包负载
//
// 供路由层 (helper -> 客户端) 使用; 空负载返回空切片而不是 nil,
// 让调用方可以直接构造包而不用再判空.
func DecodePacketData(m *PacketMessage) ([]byte, error) {
	if m == nil || m.Data == "" {
		return []byte{}, nil
	}
	return decodeBase64(m.Data)
}
