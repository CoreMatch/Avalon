// Package room 对应 net.rwhps.server.game.room
//
// 目前只有 RelayRoom. ServerRoom 依赖游戏模拟内核 (Simulation), 属于另一批次.
package room

import (
	"errors"
	"fmt"
	"sync"

	"rwhps/internal/core/thread"
	"rwhps/internal/data/global"
	"rwhps/internal/data/temp"
	"rwhps/internal/game/player"
	"rwhps/internal/io/packet"
	"rwhps/internal/net"
	"rwhps/internal/net/core"
	"rwhps/internal/struct/list"
	"rwhps/internal/struct/maps"
	"rwhps/internal/util/algorithms"
	"rwhps/internal/util/concurrent/lock"
	"rwhps/internal/util/isutils"
	"rwhps/internal/util/javastr"
	"rwhps/internal/util/log"
	"rwhps/internal/util/mathutil"
	"rwhps/internal/util/timeutil"
)

// RelayConnect 是 RelayRoom 对 GameVersionRelay 提出的最小接口 (seam)
//
// 原版 RelayRoom 直接持有 GameVersionRelay, 而 GameVersionRelay 在
// plugin/internal/headless 下, 依赖闭源的 rusted-warfare.jar, 无法移植.
// 因此这里按"消费方定义接口"的原则, 只声明 RelayRoom 真正读到的成员.
//
// GameVersionRelay 移植时会隐式满足本接口, 无需 import 本包.
// 与 net/core/server 下的 AbstractNetConnectServer 接缝是两回事: 那个面向
// Server 风味, 且由实现方定义; 这个只服务 RelayRoom.
//
// # 接口形状在 GameVersionRelay 落地时被修正过
//
// 第一版这个接口是照着"假想的实现者"写的, 写了两个 GameVersionRelay 根本没有的
// 方法 (SendSystemMessage / SendChatMessage), 还把 UseConnectionAgreement 写成了
// bool. 真正的实现者落地时这两处都必须改:
//
//   - GameVersionRelay 继承自 AbstractNetConnect, 拿到的是 sendPacket 与
//     useConnectionAgreement(): String (协议名, 例如 "UDP"/"TCP"). 原版
//     RelayRoom.sendMsg 就是 `admin!!.sendPacket(...getSystemMessagePacket(msg))` ——
//     发包是连接的通用能力, 不是"发系统消息"这种 Server 风味的专用能力.
//   - 因此这里改成 SendPacket(*packet.Packet), 由 RelayRoom 自己拼包, 与原版
//     完全一致 (原版就是 room 自己调 NetStaticData.RwHps.abstractNetPacket 拼的).
type RelayConnect interface {
	// Name 对应 name
	Name() string
	// IP 对应 ip
	IP() string
	// IPCountry 对应 ipCountry
	IPCountry() string
	// UseConnectionAgreement 对应 useConnectionAgreement
	//
	// 原版是 AbstractNetConnect.useConnectionAgreement(), 返回连接使用的协议名
	// (String), 不是布尔值.
	UseConnectionAgreement() string
	// ClientVersion 对应 clientVersion
	ClientVersion() int
	// Site 对应 site
	Site() int32
	// LastReceivedTime 对应 lastReceivedTime (Unix 毫秒)
	LastReceivedTime() int64

	// PermissionStatus 对应 permissionStatus
	PermissionStatus() core.RelayStatus
	// SetPermissionStatus 对应 permissionStatus 的 setter
	SetPermissionStatus(core.RelayStatus)

	// RegisterPlayerId 对应 registerPlayerId
	//
	// GameVersionRelay.relayRegisterConnection 要拿房间内**其它**连接的
	// registerPlayerId 做 UUID 重复检测, 因此这个成员必须在接缝上.
	RegisterPlayerId() string

	// PlayerRelay 对应 playerRelay
	//
	// GameVersionRelay.sendPacketExtractInformation 要读对方的 playerRelay
	// (team / name / site) 来回填队伍信息.
	PlayerRelay() *player.PlayerRelay
	// StartGameFlag 对应 startGameFlag
	StartGameFlag() bool
	// SetStartGameFlag 对应 startGameFlag 的 setter
	SetStartGameFlag(v bool)

	// SendPacket 对应 sendPacket(packet)
	//
	// 原版 RelayRoom.sendMsg / sendMsgChat 都是
	// `admin!!.sendPacket(NetStaticData.RwHps.abstractNetPacket.getXxxPacket(msg))`.
	SendPacket(p *packet.Packet)
	// SendRelayServerId 对应 sendRelayServerId(multicast)
	//
	// GameVersionRelay.adminMoveNew 迁移房主时要给新的房主发这个包.
	SendRelayServerId(multicast bool)
	// AddReRelayConnect 对应 addReRelayConnect()
	//
	// 同上, 房主迁移时所有连接都要重新注册.
	AddReRelayConnect()

	// Disconnect 对应 disconnect()
	Disconnect()
}

// RelayRoom 对应 RelayRoom
type RelayRoom struct {
	// GroupNet 对应 groupNet: 玩家群发池
	GroupNet *net.GroupNet

	// AbstractNetConnectIntMap 对应 abstractNetConnectIntMap: 房间ID对应的玩家
	AbstractNetConnectIntMap *maps.IntMap[RelayConnect]

	// admin 对应 admin (属性委托 ReadWriteConcurrentSynchronize)
	//
	// 原版的 set 回调会把新值强制打成 HostPermission:
	//
	//	var admin: GameVersionRelay? by ReadWriteConcurrentSynchronize(null, set = {
	//	    it?.permissionStatus = DataPermissionStatus.RelayStatus.HostPermission
	//	    it
	//	})
	admin *lock.ReadWriteConcurrentSynchronize[RelayConnect]

	// RelayKickData 对应 relayKickData: 服务端踢人的时间数据
	RelayKickData *maps.ObjectMap[string, int]
	// RelayPlayersData 对应 relayPlayersData: 服务端退出玩家数据
	RelayPlayersData *maps.ObjectMap[string, *player.PlayerRelay]

	// ReplacePlayerHex 对应 replacePlayerHex
	ReplacePlayerHex string

	// BanUnit 对应 banUnit
	BanUnit *list.Seq[string]

	// closeRoom 对应 closeRoom (属性委托 Synchronize)
	closeRoom *lock.Synchronize[bool]

	// RoomCreateTime 对应 roomCreateTime
	RoomCreateTime int

	// ServerUUID 对应 serverUuid (Data.SERVER_RELAY_UUID)
	ServerUUID string

	// InternalID 对应 internalID
	InternalID int32
	// ID 对应 id
	ID string

	// IsMod 对应 isMod
	IsMod bool

	// minSize 对应 minSize (private set)
	//
	// 名字叫 size, 实际存的是"延迟最低的那条连接的 site" —— 见 updateMinSize.
	//
	// 原版是普通 var, updateMinSize 的写入与 GameVersionRelay.adminMoveNew 的读取
	// 都不加锁, 存在与原版完全相同的数据竞争 (唯一调用方是房主迁移, 实际由单条
	// 连接驱动, 因此原版没有暴露). Go 侧保持不加锁以维持同样语义; 若将来出现真正
	// 并发的调用方, 应在这里加原子操作而不是改调用方.
	minSize int32

	// GamePacket 对应 gamePacket
	GamePacket *temp.RelayPacketCache

	// isStartGame 对应 isStartGame (属性委托 Synchronize)
	isStartGame *lock.Synchronize[bool]

	// startGameTime 对应 startGameTime (private set)
	//
	// 原版是普通 var, 只在 isStartGame 的 set 回调里写, 读方 (RelayClientCommands)
	// 不加锁. Go 侧保持同样的不同步语义.
	startGameTime int

	// AllMute 对应 allmute
	AllMute bool
	// SyncFlag 对应 syncFlag
	SyncFlag bool

	// site 对应 site (AtomicInteger)
	site *syncInt32
	// size 对应 size (AtomicInteger)
	size *syncInt32

	// disconnectMu 对应上游 GameVersionRelay.disconnect 里的 synchronized(room!!)
	//
	// 上游用房间对象自身的监视器串行化同一房间内的断开清理 (房主迁移 / 玩家退出
	// 可能并发发生). Go 没有对象监视器, 这里给房间一把专用锁, 由
	// WithDisconnectLock 暴露给 GameVersionRelay.
	disconnectMu sync.Mutex
}

// WithDisconnectLock 对应上游 GameVersionRelay.disconnect 里的 synchronized(room!!)
//
// fn 在持有房间断开锁时执行. 锁不可重入, fn 内不得再次调用本方法.
func (r *RelayRoom) WithDisconnectLock(fn func()) {
	r.disconnectMu.Lock()
	defer r.disconnectMu.Unlock()
	fn()
}

// syncInt32 对应 java.util.concurrent.atomic.AtomicInteger
//
// 只有 get / incrementAndGet / decrementAndGet / set 四个操作用得到.
type syncInt32 struct {
	mu sync.Mutex
	v  int32
}

func (a *syncInt32) get() int32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v
}

func (a *syncInt32) incrementAndGet() int32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.v++
	return a.v
}

func (a *syncInt32) decrementAndGet() int32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.v--
	return a.v
}

func (a *syncInt32) set(v int32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.v = v
}

// newRelayRoom 对应私有构造器
// RelayRoom(internalID, id, playerName, isMod, betaGameVersion, version, maxPlayer)
//
// 原版的 playerName / betaGameVersion / version / maxPlayer 四个参数**没有被存下来**,
// 是遗留的无用参数. 照搬: 签名保留, 函数体忽略.
func newRelayRoom(internalID int32, id string, _ string, isMod bool, _ bool, _ int, _ int) *RelayRoom {
	r := &RelayRoom{
		GroupNet:                 net.NewGroupNet(),
		AbstractNetConnectIntMap: maps.NewIntMapCap[RelayConnect](16, true),
		RelayKickData:            maps.NewObjectMap[string, int](false),
		RelayPlayersData:         maps.NewObjectMap[string, *player.PlayerRelay](false),
		BanUnit:                  list.NewSeq[string](),
		RoomCreateTime:           timeutil.ConcurrentSecond(),
		ServerUUID:               global.ServerRelayUUID,
		InternalID:               internalID,
		ID:                       id,
		IsMod:                    isMod,
		GamePacket:               temp.NewRelayPacketCache(),
		SyncFlag:                 true,
		site:                     &syncInt32{},
		size:                     &syncInt32{},
	}

	// 原版属性初始化的顺序: admin 的 set 回调引用 isStartGame, 因此先建 isStartGame
	r.isStartGame = lock.NewSynchronizeWith(false, nil, func(old, isStart bool) bool {
		// 原版: if (isStartGame && it) return it —— 已经开局又设为开局时不动 startGameTime.
		// Go 的锁不可重入, 旧值由 set 回调的 old 参数提供 (见 lock 包的说明).
		if old && isStart {
			return isStart
		}
		if isStart {
			r.startGameTime = timeutil.ConcurrentSecond() + 300
		} else {
			r.startGameTime = 0
		}
		return isStart
	})

	r.admin = lock.NewReadWriteConcurrentSynchronizeWith[RelayConnect](nil, nil, func(_, newAdmin RelayConnect) RelayConnect {
		if newAdmin != nil {
			newAdmin.SetPermissionStatus(core.RelayHostPermission)
		}
		return newAdmin
	})

	r.closeRoom = lock.NewSynchronize(false)

	// 原版私有构造器的第一行: serverRelayDataRoom[internalID] = this
	serverRelayDataRoom.Put(internalID, r)
	return r
}

// newSingleRoom 对应内部构造器 RelayRoom(id, groupNet)
//
// 仅供 NetStaticData.relayRoom 的单房间模式使用: internalID 固定为 0,
// 不注册进 serverRelayDataRoom (原版同样不注册).
func newSingleRoom(id string, groupNet *net.GroupNet) *RelayRoom {
	r := &RelayRoom{
		GroupNet:                 groupNet,
		AbstractNetConnectIntMap: maps.NewIntMapCap[RelayConnect](16, true),
		RelayKickData:            maps.NewObjectMap[string, int](false),
		RelayPlayersData:         maps.NewObjectMap[string, *player.PlayerRelay](false),
		BanUnit:                  list.NewSeq[string](),
		RoomCreateTime:           timeutil.ConcurrentSecond(),
		ServerUUID:               global.ServerRelayUUID,
		InternalID:               0,
		ID:                       id,
		GamePacket:               temp.NewRelayPacketCache(),
		SyncFlag:                 true,
		site:                     &syncInt32{},
		size:                     &syncInt32{},
	}
	r.isStartGame = lock.NewSynchronizeWith(false, nil, func(old, isStart bool) bool {
		if old && isStart {
			return isStart
		}
		if isStart {
			r.startGameTime = timeutil.ConcurrentSecond() + 300
		} else {
			r.startGameTime = 0
		}
		return isStart
	})
	r.admin = lock.NewReadWriteConcurrentSynchronizeWith[RelayConnect](nil, nil, func(_, newAdmin RelayConnect) RelayConnect {
		if newAdmin != nil {
			newAdmin.SetPermissionStatus(core.RelayHostPermission)
		}
		return newAdmin
	})
	r.closeRoom = lock.NewSynchronize(false)
	return r
}

// NewSingleRoom 对应内部构造器 RelayRoom(id, groupNet), 供 NetStaticData 使用
//
// 原版是 internal, Go 侧没有 internal 可见性, 因此导出并在注释里标明用途.
func NewSingleRoom(id string, groupNet *net.GroupNet) *RelayRoom {
	return newSingleRoom(id, groupNet)
}

var (
	singleRoomOnce sync.Once
	singleRoom     *RelayRoom
)

// SingleRoom 对应 NetStaticData.relayRoom (单房间模式)
//
// 原版:
//
//	val relayRoom = RelayRoom("RW-HPS Beta Relay", groupNet)
//	    get() {
//	        field.closeRoom = false
//	        return field
//	    }
//
// 即: 全局唯一一个房间, 复用 NetStaticData.groupNet 作为群发池, 而且**每次访问
// 都会把 closeRoom 重置为 false** —— 房间被关掉后再取一次就等于重新开放.
//
// # 与原版的位置差异
//
// 原版它挂在 NetStaticData (net 包) 上. Go 侧不能这么做: room 包依赖 net 包
// (Room.groupNet 是 *net.GroupNet), 若 net 再依赖 room 就成环. 因此把单例放到
// room 包, 由 room 自己去取 net.NetStaticData.GroupNet.
//
// 后果: net.NetStaticData 上没有 RelayRoom 字段, 需要单房间的调用方走
// room.SingleRoom().
func SingleRoom() *RelayRoom {
	singleRoomOnce.Do(func() {
		singleRoom = newSingleRoom("RW-HPS Beta Relay", net.NetStaticData.GroupNet)
	})
	// 对应原版 getter 里的 `field.closeRoom = false`
	singleRoom.SetCloseRoom(false)
	return singleRoom
}

// Admin 对应 admin
func (r *RelayRoom) Admin() RelayConnect { return r.admin.Get() }

// SetAdmin 对应 admin = ...
func (r *RelayRoom) SetAdmin(a RelayConnect) { r.admin.Set(a) }

// CloseRoom 对应 closeRoom
func (r *RelayRoom) CloseRoom() bool { return r.closeRoom.Get() }

// SetCloseRoom 对应 closeRoom = ...
func (r *RelayRoom) SetCloseRoom(v bool) { r.closeRoom.Set(v) }

// IsStartGame 对应 isStartGame
func (r *RelayRoom) IsStartGame() bool { return r.isStartGame.Get() }

// SetIsStartGame 对应 isStartGame = ...
func (r *RelayRoom) SetIsStartGame(v bool) { r.isStartGame.Set(v) }

// StartGameTime 对应 startGameTime (private set)
func (r *RelayRoom) StartGameTime() int { return r.startGameTime }

// MinSize 对应 minSize (private set)
func (r *RelayRoom) MinSize() int32 { return r.minSize }

// AllIP 对应 allIP
//
// 原版用 admin!!, admin 为空时抛 NullPointerException. Go 侧保留同样的硬失败,
// 但给出可读的 panic 信息.
func (r *RelayRoom) AllIP() string {
	admin := r.adminOrPanic()

	var b []byte
	b = append(b, global.LineSeparator...)
	b = append(b, admin.Name()...)
	b = append(b, " / IP: "...)
	b = append(b, admin.IP()...)
	b = append(b, " / Protocol: "...)
	b = append(b, fmt.Sprintf("%v", admin.UseConnectionAgreement())...)
	b = append(b, " / Admin: true"...)

	r.AbstractNetConnectIntMap.EachAll(func(_ int32, e RelayConnect) {
		b = append(b, global.LineSeparator...)
		b = append(b, e.Name()...)
		b = append(b, " / IP: "...)
		b = append(b, e.IP()...)
		b = append(b, " / Protocol: "...)
		b = append(b, fmt.Sprintf("%v", e.UseConnectionAgreement())...)
		b = append(b, " / Admin: false"...)
	})
	return string(b)
}

func (r *RelayRoom) adminOrPanic() RelayConnect {
	a := r.admin.Get()
	if a == nil {
		panic("room: admin is null (Kotlin: admin!!)")
	}
	return a
}

// GetAbstractNetConnect 对应 getAbstractNetConnect(site)
func (r *RelayRoom) GetAbstractNetConnect(site int32) (RelayConnect, bool) {
	return r.AbstractNetConnectIntMap.Get(site)
}

// SetAbstractNetConnect 对应 setAbstractNetConnect(abstractNetConnect)
func (r *RelayRoom) SetAbstractNetConnect(c RelayConnect) {
	r.AbstractNetConnectIntMap.Put(c.Site(), c)
}

// RemoveAbstractNetConnect 对应 removeAbstractNetConnect(site)
func (r *RelayRoom) RemoveAbstractNetConnect(site int32) {
	r.AbstractNetConnectIntMap.Remove(site)
}

// relayPacketBuilder 对应 NetStaticData.RwHps.abstractNetPacket 里 room 真正用到的两个方法
type relayPacketBuilder interface {
	GetSystemMessagePacket(msg string) (*packet.Packet, error)
	GetChatMessagePacket(msg string, sendBy string, team int) (*packet.Packet, error)
}

// abstractNetPacketProvider 取当前服务端协议的包构造器
//
// 原版直接写 NetStaticData.RwHps.abstractNetPacket. Go 侧多一层间接, 原因是
// NetStaticData.RwHps() 是 sync.Once 缓存的单例, 单测里装不进一个假的构造器.
//
// 返回值是 netpacket.AbstractNetPacket 的子集接口, 这样单测只需要实现两个方法.
var abstractNetPacketProvider = func() relayPacketBuilder {
	return net.NetStaticData.RwHps().AbstractNetPacket()
}

// SendMsg 对应 sendMsg(msg)
//
// 原版: admin!!.sendPacket(NetStaticData.RwHps.abstractNetPacket.getSystemMessagePacket(msg)),
// 外面包着 try/catch(Exception) 只 printStackTrace.
//
// 拼包在 room 侧完成 (与原版一致), 而不是让连接自己去拼 —— 连接只有
// sendPacket 这一个通用能力.
func (r *RelayRoom) SendMsg(msg string) {
	admin := r.admin.Get()
	if admin == nil {
		// 原版这里是 admin!!, 会抛 NullPointerException
		panic("room: admin is null (Kotlin: admin!!)")
	}

	builder := abstractNetPacketProvider()
	if builder == nil {
		log.Errorf("[RelayRoom] Send System Chat Error", errors.New("no abstractNetPacket available"))
		return
	}
	p, err := builder.GetSystemMessagePacket(msg)
	if err != nil {
		log.Errorf("[RelayRoom] Send System Chat Error", err)
		return
	}
	admin.SendPacket(p)
}

// SendMsgChat 对应 sendMsg(msg, msgName, team)
func (r *RelayRoom) SendMsgChat(msg, msgName string, team int) {
	admin := r.admin.Get()
	if admin == nil {
		panic("room: admin is null (Kotlin: admin!!)")
	}

	builder := abstractNetPacketProvider()
	if builder == nil {
		log.Errorf("[RelayRoom] Send Chat Error", errors.New("no abstractNetPacket available"))
		return
	}
	p, err := builder.GetChatMessagePacket(msg, msgName, team)
	if err != nil {
		log.Errorf("[RelayRoom] Send Chat Error", err)
		return
	}
	admin.SendPacket(p)
}

// GetActiveConnectionSize 对应 getActiveConnectionSize()
func (r *RelayRoom) GetActiveConnectionSize() int {
	return r.AbstractNetConnectIntMap.Size()
}

// GetPosition 对应 getPosition()
func (r *RelayRoom) GetPosition() int32 { return r.site.get() }

// SetAddPosition 对应 setAddPosition()
func (r *RelayRoom) SetAddPosition() int32 { return r.site.incrementAndGet() }

// GetSize 对应 getSize()
func (r *RelayRoom) GetSize() int32 { return r.size.get() }

// SetAddSize 对应 setAddSize()
func (r *RelayRoom) SetAddSize() { r.size.incrementAndGet() }

// SetRemoveSize 对应 setRemoveSize()
func (r *RelayRoom) SetRemoveSize() { r.size.decrementAndGet() }

// UpdateMinSize 对应 updateMinSize()
//
// 原版逻辑: 以"当前时间 - 每条连接的 lastReceivedTime"为键、site 为值建一张
// ObjectIntMap, 然后取键最小的那一项的**值**赋给 minSize. 所以 minSize 名字叫
// size, 实际存的是延迟最低那条连接的 site. 延迟相同的连接会互相覆盖, 只留最后一条.
//
// 整个方法体包在 try/catch(Exception) 里, 出错只记日志. Go 侧用 recover 等效.
func (r *RelayRoom) UpdateMinSize() {
	defer func() {
		if e := recover(); e != nil {
			log.Errorf("[RELAY updateMinSize]", fmt.Errorf("%v", e))
		}
	}()

	if r.AbstractNetConnectIntMap.Size() < 1 {
		return
	}

	time := timeutil.ConcurrentMillis()
	latency := maps.NewObjectIntMap[int32](false)
	r.AbstractNetConnectIntMap.EachAll(func(site int32, con RelayConnect) {
		latency.Put(int32(time-con.LastReceivedTime()), site)
	})

	// 对应 it.toArrayKey().toIntArray().min()
	keys := latency.Keys()
	minKey := keys[0]
	for _, k := range keys[1:] {
		if k < minKey {
			minKey = k
		}
	}
	// 对应 it[minKey]!!
	r.minSize, _ = latency.Get(minKey)
}

// Re 对应 re()
func (r *RelayRoom) Re() { r.RemoveRoom(nil) }

// RemoveRoom 对应 removeRoom(run)
func (r *RelayRoom) RemoveRoom(run func()) {
	// 原版 catch (e: Exception) { // Ignore }: 清理过程中的任何异常都被吞掉,
	// 但 finally 里的注销和 run() 照样执行.
	func() {
		defer func() { _ = recover() }()
		r.GroupNet.Disconnect()
		if a := r.admin.Get(); a != nil {
			a.Disconnect()
		}
		r.AbstractNetConnectIntMap.EachAll(func(_ int32, con RelayConnect) {
			con.Disconnect()
		})
		r.closeRoom.Set(true)
		r.AbstractNetConnectIntMap.Clear()
		r.site.set(0)
		r.admin.Set(nil)
		r.isStartGame.Set(false)
	}()

	serverRelayDataRoom.Remove(r.InternalID)
	if run != nil {
		run()
	}
}

// Equals 对应 equals(other)
//
// 原版的判等很特殊: 比的是 this.internalID 与 **对方** id 换算出来的 internalID,
// 完全不比 this.id. 照搬.
func (r *RelayRoom) Equals(other any) bool {
	if other == nil {
		return false
	}
	if r == other {
		return true
	}
	o, ok := other.(*RelayRoom)
	if !ok {
		// 对应 javaClass != other.javaClass
		return false
	}
	id, err := CoverRelayID(o.ID)
	if err != nil {
		return false
	}
	return r.InternalID == id
}

// HashCode 对应 hashCode() = Objects.hash(id)
//
// Objects.hash(单参) = 31 * 1 + id.hashCode()
func (r *RelayRoom) HashCode() int32 {
	return 31 + javastr.HashCode(r.ID)
}

// String 对应 Kotlin data class 风格的默认 toString 不存在, 这里给一个可读形式
func (r *RelayRoom) String() string {
	return fmt.Sprintf("RelayRoom{internalID=%d, id=%s}", r.InternalID, r.ID)
}

// ---------------------------------------------------------------------------
// 伴生对象 (companion object)
// ---------------------------------------------------------------------------

var (
	// ServerRelayOld 对应 serverRelayOld: registerPlayerId -> 房间ID 的映射
	//
	// 消费者是 GameVersionRelay (尚未移植), 因此导出.
	ServerRelayOld = maps.NewObjectMap[string, string](false)

	// ServerRelayIpData 对应 serverRelayIpData: Seq<String>(true) 线程安全
	ServerRelayIpData = list.NewSeqCap[string](16, true)

	// serverRelayDataRoom 对应 serverRelayDataRoom: IntMap<RelayRoom>(128, true)
	serverRelayDataRoom = maps.NewIntMapCap[*RelayRoom](128, true)

	// rand 对应 rand = Rand()
	rand = mathutil.NewRand()
)

// RelayAllIP 对应 relayAllIP
func RelayAllIP() string {
	var b []byte
	serverRelayDataRoom.EachAll(func(_ int32, e *RelayRoom) {
		b = append(b, global.LineSeparator...)
		b = append(b, e.ID...)
		b = append(b, e.AllIP()...)
	})
	return string(b)
}

// AllSize 对应 allSize
func AllSize() int32 {
	var total int32
	serverRelayDataRoom.EachAll(func(_ int32, e *RelayRoom) {
		total += e.GetSize()
	})
	return total
}

// RoomAllSize 对应 roomAllSize
func RoomAllSize() int { return serverRelayDataRoom.Size() }

// RoomNoStartSize 对应 roomNoStartSize
func RoomNoStartSize() int32 {
	var n int32
	serverRelayDataRoom.EachAll(func(_ int32, e *RelayRoom) {
		if !e.IsStartGame() {
			n++
		}
	})
	return n
}

// RoomPublicSize 对应 roomPublicSize: 原版恒返回 0
func RoomPublicSize() int { return 0 }

// RandPow 对应 randPow (@get:Synchronized)
//
// 原版每次访问都新建一个 NetConnectProofOfWork, 由 JVM 的同步锁串行化.
// Go 侧用互斥锁达到同样的串行化效果.
var randPowMu sync.Mutex

func RandPow() *algorithms.NetConnectProofOfWork {
	randPowMu.Lock()
	defer randPowMu.Unlock()
	return algorithms.NewNetConnectProofOfWork()
}

// GetRelay 对应 getRelay(id)
//
// 返回值里的 error 对应原版 id.toInt() 可能抛出的 NumberFormatException:
// isNumeric 的正则允许小数 (如 "1.5"), 而 id.length < 10 只挡住 10 位以上的溢出,
// 所以 "1.5" 这种房间 ID 会在原版里炸掉. Go 侧改为返回错误, 不 panic.
func GetRelay(id string) (*RelayRoom, error) {
	internalID, err := CoverRelayID(id)
	if err != nil {
		return nil, err
	}
	r, _ := serverRelayDataRoom.Get(internalID)
	return r, nil
}

// GetCheckRelay 对应 getCheckRelay(id)
func GetCheckRelay(id string) (bool, error) {
	internalID, err := CoverRelayID(id)
	if err != nil {
		return false, err
	}
	return serverRelayDataRoom.ContainsKey(internalID), nil
}

// SendAllMsg 对应 sendAllMsg(msg)
func SendAllMsg(msg string) {
	thread.NewThreadOther(func() {
		serverRelayDataRoom.EachAll(func(_ int32, e *RelayRoom) {
			e.SendMsg(msg)
		})
	})
}

// GetAllRelayIpCountry 对应 getAllRelayIpCountry()
func GetAllRelayIpCountry() map[string]int {
	temp := map[string]int{}
	bump := func(country string) { temp[country]++ }

	serverRelayDataRoom.EachAll(func(_ int32, relay *RelayRoom) {
		bump(relay.adminOrPanic().IPCountry())
		relay.AbstractNetConnectIntMap.EachAll(func(_ int32, con RelayConnect) {
			bump(con.IPCountry())
		})
	})
	return temp
}

// GetAllRelayVersion 对应 getAllRelayVersion()
func GetAllRelayVersion() map[int]int {
	temp := map[int]int{}
	bump := func(version int) { temp[version]++ }

	serverRelayDataRoom.EachAll(func(_ int32, relay *RelayRoom) {
		bump(relay.adminOrPanic().ClientVersion())
		relay.AbstractNetConnectIntMap.EachAll(func(_ int32, con RelayConnect) {
			bump(con.ClientVersion())
		})
	})
	return temp
}

// CleanRoom 对应 cleanRoom()
func CleanRoom() {
	time := timeutil.ConcurrentSecond() - 12*60*60
	cleanSeq := list.NewSeq[*RelayRoom]()

	serverRelayDataRoom.EachAll(func(_ int32, value *RelayRoom) {
		if value.RoomCreateTime < time || value.CloseRoom() {
			cleanSeq.Add(value)
		}
	})
	cleanSeq.EachAll(func(r *RelayRoom) { r.Re() })
}

// CoverRelayID 对应 coverRelayID(id)
//
// 数字且长度 < 10 时直接取整; 否则取 hashCode 并在其为正时取负.
// 原版的 id.toInt() 在 "1.5" 这类输入上会抛 NumberFormatException (isNumeric 的
// 正则允许小数), Go 侧把同一个失败变成 error.
func CoverRelayID(id string) (int32, error) {
	if isutils.IsNumeric(id) && len(id) < 10 {
		v, err := parseJavaInt(id)
		if err != nil {
			return 0, err
		}
		return v, nil
	}
	h := javastr.HashCode(id)
	if h > 0 {
		return -h, nil
	}
	return h, nil
}

// parseJavaInt 对应 Kotlin 的 String.toInt() / Java 的 Integer.parseInt
func parseJavaInt(s string) (int32, error) {
	var v int64
	neg := false
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	if i == len(s) {
		return 0, fmt.Errorf("room: invalid int %q", s)
	}
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("room: invalid int %q", s)
		}
		v = v*10 + int64(c-'0')
		if v > 2147483648 {
			return 0, fmt.Errorf("room: int overflow %q", s)
		}
	}
	if neg {
		v = -v
	}
	if v < -2147483648 || v > 2147483647 {
		return 0, fmt.Errorf("room: int overflow %q", s)
	}
	return int32(v), nil
}

// getRelayMu 对应 @Synchronized: getRelay(...) 的实例工厂是串行的
var getRelayMu sync.Mutex

// GetRelayNew 对应 getRelay(id, playerName, isMod, betaGameVersion, version, maxPlayer)
//
// id 为空时自动生成一个 1000..100000 之间未被占用的数字 ID.
// 原版标记为 "This method should not be open to the public for internal use only".
func GetRelayNew(id, playerName string, isMod, betaGameVersion bool, version, maxPlayer int) *RelayRoom {
	getRelayMu.Lock()
	defer getRelayMu.Unlock()

	idRelay := id
	if idRelay == "" {
		for {
			intID := rand.RandomInt(1000, 100000)
			taken, _ := GetCheckRelay(itoa(intID))
			if !taken {
				idRelay = itoa(intID)
				log.Debug(intID)
				break
			}
		}
	}
	internalID, err := CoverRelayID(idRelay)
	if err != nil {
		// 原版此处会抛 NumberFormatException; 调用方传进来的 id 若形如 "1.5"
		// 就会炸. Go 侧退回到用 hashCode 派生, 不静默也不 panic.
		internalID, _ = CoverRelayID(fmt.Sprintf("relay-%s", idRelay))
	}
	return newRelayRoom(internalID, idRelay, playerName, isMod, betaGameVersion, version, maxPlayer)
}

// itoa 对应 Int.toString()
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// 下面是包内测试用的钩子, 对应原版伴生对象的 private 成员
func resetForTest() {
	serverRelayDataRoom = maps.NewIntMapCap[*RelayRoom](128, true)
	ServerRelayOld = maps.NewObjectMap[string, string](false)
	ServerRelayIpData = list.NewSeqCap[string](16, true)
	rand = mathutil.NewRand()
}
