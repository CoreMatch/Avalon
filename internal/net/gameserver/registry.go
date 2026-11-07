package gameserver

import (
	"encoding/binary"
	"sync"

	"github.com/CoreMatch/Avalon/internal/data/global"
	"github.com/CoreMatch/Avalon/internal/game/event"
	"github.com/CoreMatch/Avalon/internal/game/external"
	"github.com/CoreMatch/Avalon/internal/game/player"
	"github.com/CoreMatch/Avalon/internal/game/room"
	"github.com/CoreMatch/Avalon/internal/io/packet"
	"github.com/CoreMatch/Avalon/internal/io/packettype"
	"github.com/CoreMatch/Avalon/internal/util/log"
)

// HelperBackend 是连接层使用外部游戏内核的最小接口 (conn.* 三件套)
//
// 对应上游"连接与引擎之间"的那条路: netEngine.aM.add / netEnginePackaging.addPacket
// 在 Go 架构里被压成三条显式操作. *external.Backend 是它的主要实现;
// 接口放在消费方 (本包), 测试可以注入记录型假实现.
type HelperBackend interface {
	// ConnOpen 登记一条新连接
	ConnOpen(room, conn string) error
	// ConnClose 关闭一条连接
	ConnClose(room, conn string) error
	// ConnPacket 把客户端包转给引擎
	ConnPacket(room, conn string, p *packet.Packet) error
}

var (
	helperMu sync.RWMutex
	helper   HelperBackend
)

// SetHelper 设置连接层使用的外部内核; 传 nil 恢复无后端状态
//
// 对应上游 GameEngine.protocolProvider 的赋值时机: 引擎就绪时装上, 关闭时清空.
func SetHelper(h HelperBackend) {
	helperMu.Lock()
	helper = h
	helperMu.Unlock()
}

func currentHelper() HelperBackend {
	helperMu.RLock()
	defer helperMu.RUnlock()
	return helper
}

// AttachBackend 由启动链调用: 装上内核并登记"引擎 -> 客户端"的路由回调
//
// 对应上游 NetEnginePackaging 的角色: 引擎产出的包按连接找到客户端并发出去.
// 主动消息里的 Conn 即 Go 侧分配的 connID (见 NewGameVersionServer).
func AttachBackend(b *external.Backend) {
	SetHelper(b)
	if b == nil {
		return
	}
	b.SetHandlers(external.Handlers{
		Packet: DeliverPacket,
		// 玩家档案 / 进房 / 离房事件: 驱动房间实体链 (里程碑 2).
		// 对应上游引擎经 PlayerConnectX 回调 RW-HPS 的那条路.
		Event: handleHelperEvent,
		// 旧一代 helper 承载着所有客户端连接, 它一死这些连接全部作废:
		// 与上游"引擎进程消失即断开全部玩家"一致.
		HelperExit: func(gen uint64) {
			log.Errorf("[GameServer]", "helper generation {0} exited, dropping all game connections", gen)
			disconnectAll()
		},
		// 新一代 helper 就绪: 房间配置重建属里程碑 2 (房间实体链) 的职责,
		// 且上游语义明确要求"不要自动开局".
		HelperReady: func(uint64) {},
	})
}

// handleHelperEvent 处理引擎发来的玩家事件
//
// 对应上游引擎 -> PlayerConnectX -> room.playerManage 的那条路:
//   - player.join    : addAbstractPlayer 并绑定到对应连接, 随后 fire PlayerJoinEvent
//   - player.leave   : 断开该连接 (disconnect 内部完成房间列表清理与 PlayerLeaveEvent)
//   - player.update  : 刷新档案 (队伍 / 准备状态)
func handleHelperEvent(ev *external.EventMessage) {
	r := room.Default()
	if r == nil {
		return
	}

	switch ev.Event {
	case external.EventPlayerJoin:
		profile, err := external.DecodePlayerProfile(ev)
		if err != nil {
			log.Errorf("[GameServer]", "decode player.join: {0}", err)
			return
		}
		g := connections.get(ev.Conn)
		if g == nil {
			log.Warnf("[GameServer]", "player.join for unknown conn {0}", ev.Conn)
			return
		}
		data := player.NewData()
		data.Apply(func(d *player.Data) {
			d.ConnectHexID = profile.ID
			d.Name = profile.Name
			d.Team = profile.Team
			d.Host = profile.Host
			d.Bot = profile.Bot
			d.Spectator = profile.Spectator
		})
		p := r.PlayerManage.AddAbstractPlayer(g, data, i18nInput)
		g.SetPlayer(p)

		// 上游 PlayerConnectX 在 addAbstractPlayer 后立刻 fire PlayerJoinEvent
		// (名字校验 / 封禁踢出 / 自动开局等默认监听都挂在这里).
		r.EventManage.Fire(&event.PlayerJoinEvent{Player: p})

	case external.EventPlayerLeave:
		if g := connections.get(ev.Conn); g != nil {
			g.Disconnect()
		}

	case external.EventPlayerUpdate:
		profile, err := external.DecodePlayerProfile(ev)
		if err != nil {
			log.Errorf("[GameServer]", "decode player.update: {0}", err)
			return
		}
		if g := connections.get(ev.Conn); g != nil && g.Player() != nil {
			d := g.Player().Data()
			d.Apply(func(dd *player.Data) {
				dd.Team = profile.Team
				dd.Name = profile.Name
				dd.Host = profile.Host
				dd.Bot = profile.Bot
				dd.Spectator = profile.Spectator
			})
		}
	}
}

// i18nInput 对应 Data.i18NBundle.getinput, 供玩家实体取词
func i18nInput(key string, params ...any) string {
	if global.I18NBundle == nil {
		return key
	}
	return global.I18NBundle.GetInput(key, params...)
}

// DeliverPacket 把引擎发来的一个游戏包路由给对应客户端
//
// 这是 AttachBackend 登记的回调, 单独导出是为了集成测试与未来
// 多房间路由的接线可以复用同一入口.
func DeliverPacket(m *external.PacketMessage) { routePacket(m) }

// routePacket 把引擎发来的游戏包路由给对应客户端
//
// 对应上游"引擎 -> 客户端"的发包路径 (NetEnginePackaging.transformPacket 之后
// 的 send). 找不到连接只记日志: 连接可能刚好在包飞行途中关闭.
//
// 协议 v4 (地图分发): START_GAME(120) 走专门路径 —— 只读解析负载前缀判断
// 是否携带地图流, 发送失败归类为"传输中断", 成功送达记入连接状态供
// "客户端拒收"分类. 其余包类型保持原有透传.
func routePacket(m *external.PacketMessage) {
	g := connections.get(m.Conn)
	if g == nil {
		log.Warnf("[GameServer]", "packet for unknown conn {0}, dropped", m.Conn)
		return
	}
	data, err := external.DecodePacketData(m)
	if err != nil {
		log.Errorf("[GameServer]", "decode packet data for conn {0}: {1}", m.Conn, err)
		return
	}
	pkt := packet.NewPacketFromInt(m.PType, data)
	if m.PType == int(packettype.START_GAME) {
		routeStartGame(g, pkt, data)
		return
	}
	g.SendPacket(pkt)
}

// routeStartGame 发送 START_GAME 并做地图分发观测 (协议 v4).
//
// 负载前缀 (引擎官方格式, 只读不改写): [byte][int32 地图类型枚举]
// (枚举 1 = 自定义地图) [int32 地图流大小][地图字节]... 解析失败不影响
// 发送 —— 分类是尽力而为, 透传是硬职责.
func routeStartGame(g *GameVersionServer, pkt *packet.Packet, data []byte) {
	if err := g.SendStartGame(pkt); err != nil {
		log.Errorf("[GameServer]", "map delivery: transfer interrupted (conn {0}): START_GAME write failed: {1}", g.ConnID(), err)
		g.Disconnect()
		return
	}
	size := parseStartGameMapStream(data)
	g.noteMapStream(size)
}

// parseStartGameMapStream 只读解析 START_GAME 负载里的地图流大小.
//
// 返回 -1: 非自定义地图开局 (无地图流) 或负载不足以解析.
// 自定义地图分支返回声明的地图字节数.
func parseStartGameMapStream(data []byte) int64 {
	if len(data) < 9 || data[0] != 0 {
		return -1
	}
	ordinal := int32(binary.BigEndian.Uint32(data[1:5]))
	if ordinal != 1 {
		return -1
	}
	return int64(int32(binary.BigEndian.Uint32(data[5:9])))
}

// connRegistry 连接表: helper 侧 connID -> GameVersionServer
type connRegistry struct {
	mu sync.RWMutex
	m  map[string]*GameVersionServer
}

var connections = &connRegistry{m: make(map[string]*GameVersionServer)}

func (r *connRegistry) add(g *GameVersionServer) {
	if g == nil {
		return
	}
	r.mu.Lock()
	r.m[g.connID] = g
	r.mu.Unlock()
}

func (r *connRegistry) remove(connID string) {
	r.mu.Lock()
	delete(r.m, connID)
	r.mu.Unlock()
}

func (r *connRegistry) get(connID string) *GameVersionServer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m[connID]
}

func (r *connRegistry) all() []*GameVersionServer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*GameVersionServer, 0, len(r.m))
	for _, g := range r.m {
		out = append(out, g)
	}
	return out
}

// disconnectAll 断开全部游戏连接
//
// 触发点: helper 进程退出 (见 AttachBackend 的 HelperExit).
func disconnectAll() {
	for _, g := range connections.all() {
		g.Disconnect()
	}
}
