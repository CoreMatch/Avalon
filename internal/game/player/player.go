package player

import (
	"errors"
	"strconv"
	"sync"

	"rwhps/internal/io/packet"
	"rwhps/internal/util/isutils"
	"rwhps/internal/util/javastr"
	"rwhps/internal/util/log"
	"rwhps/internal/util/timeutil"
)

// ErrPlayerNotBound 对应 ImplementedException.PlayerImplementedException("[Player] No Bound Connection")
var ErrPlayerNotBound = errors.New("[Player] No Bound Connection")

// ErrNetDomain 对应 NetException("[ERROR_DOMAIN] Error Domain")
var ErrNetDomain = errors.New("[ERROR_DOMAIN] Error Domain")

// ErrNetConnectClosed 对应 NetException("[CONNECT_CLOSE] Connect disconnect")
var ErrNetConnectClosed = errors.New("[CONNECT_CLOSE] Connect disconnect")

// PlayerConnect 是玩家实体对连接的最小依赖面 (消费方定义的接缝).
//
// 上游 PlayerHess.con 的类型是 AbstractNetConnectServer (net.core.server),
// 而 Go 里 game.player 不能反向依赖 net 层 (会成环), 因此只声明玩家真正
// 用到的成员; net/gameserver.GameVersionServer 天然满足该接口.
//
// 命名与上游方法一一对应: sendSystemMessage / sendChatMessage / sendKick /
// sendRelayServerType (sendPopUps 的底层) / sendPacket.
type PlayerConnect interface {
	SendSystemMessage(text string) error
	SendChatMessage(msg, sendBy string, team int) error
	SendKick(reason string) error
	SendRelayServerType(msg string, run func(string))
	SendPacket(p *packet.Packet)
	// SendJumpToAnotherServer 对应 playerJumpsToAnotherServer 里的发包动作.
	//
	// 跳转包 (fromRelayJumpsToAnotherServerInternalPacket) 的构造在 net 层
	// (依赖 internal/relay), player 包不能反向导入, 故委托给连接实现.
	SendJumpToAnotherServer(ipport string)
	// IP / UseConnectionAgreement / IPCountry 供 playerInfo / infoObject 展示
	IP() string
	UseConnectionAgreement() string
	IPCountry() string
}

// Player 对应 PlayerHess: 服务端模式下的玩家实体
//
// 上游字段逐一保留; 数据类字段 (名字/队伍/战绩) 委托给镜像 Data,
// 管理类字段 (admin/禁言/踢出) 由 Go 侧持有.
type Player struct {
	mu sync.RWMutex

	// con 对应 con (Synchronize 包装), 未绑定时为 nil
	con PlayerConnect

	// i18n 对应 i18NBundle
	i18n func(key string, params ...any) string

	// data 对应 playerPrivateData
	data *Data

	// IsAdmin 对应 isAdmin (@Volatile)
	IsAdmin bool
	// AutoAdmin 对应 autoAdmin: 自动分配的 ADMIN 标记
	AutoAdmin bool
	// SuperAdmin 对应 superAdmin
	SuperAdmin bool
	// Never 对应 never (NeverEndGame 插件用)
	Never bool

	// LastMoveTime 对应 lastMoveTime (@Volatile)
	LastMoveTime int

	// MuteTime 对应 muteTime: 禁言到期 (毫秒)
	MuteTime int64
	// KickTime 对应 kickTime: 踢出到期 (毫秒)
	KickTime int64
	// TimeTemp 对应 timeTemp
	TimeTemp int64
	// LastMessageTime 对应 lastMessageTime
	LastMessageTime int64
	// LastSentMessage 对应 lastSentMessage
	LastSentMessage string
	// NoSay 对应 noSay
	NoSay bool

	// RemoveOnKick 对应 AiPlayer.kickPlayer 覆盖里"先自移除再踢"的前置动作.
	//
	// 上游靠 Kotlin 虚分派实现覆盖; Go 无虚方法, 用钩子字段表达同一语义:
	// 普通玩家为 nil (上游 PlayerHess.kickPlayer 也没有这一步),
	// AiPlayer 在构造时注入. 管理层按 *Player 存储时行为依然正确.
	RemoveOnKick func()

	// customData 对应 customData: 插件/命令暂存的自定义数据
	customMu   sync.Mutex
	customData map[string]any
}

// New 对应 PlayerHess(con, i18NBundle, playerPrivateData)
//
// i18n 传 nil 时 getinput 原样返回 key (语言包缺失的兜底, 与 app.tr 一致).
func New(con PlayerConnect, i18n func(key string, params ...any) string, data *Data) *Player {
	if data == nil {
		data = NewData()
	}
	return &Player{
		con:        con,
		i18n:       i18n,
		data:       data,
		customData: make(map[string]any),
	}
}

// Con 返回当前绑定的连接 (可能为 nil)
func (p *Player) Con() PlayerConnect {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.con
}

// SetCon 对应 con 的重新绑定 (重连时由 PlayerManage 调用)
func (p *Player) SetCon(con PlayerConnect) {
	p.mu.Lock()
	p.con = con
	p.mu.Unlock()
}

// Data 返回玩家数据镜像
func (p *Player) Data() *Data { return p.data }

// --- 委托给 Data 的字段 (对应上游 by playerPrivateData::xxx) ---

// Index 对应 index
func (p *Player) Index() int { return p.data.GetIndex() }

// SetIndex 对应 index 写
func (p *Player) SetIndex(v int) { p.data.SetIndex(v) }

// Position 对应 position: index + 1
func (p *Player) Position() int { return p.Index() + 1 }

// SetPosition 对应 position 写: index = value - 1
func (p *Player) SetPosition(v int) { p.SetIndex(v - 1) }

// Team 对应 team
func (p *Player) Team() int { return p.data.GetTeam() }

// SetTeam 对应 team 写
func (p *Player) SetTeam(v int) { p.data.SetTeam(v) }

// Credits 对应 credits
func (p *Player) Credits() int { return p.data.GetCredits() }

// SetCredits 对应 credits 写
func (p *Player) SetCredits(v int) { p.data.SetCredits(v) }

// StartUnit 对应 startUnit
func (p *Player) StartUnit() int { return p.data.GetStartUnit() }

// SetStartUnit 对应 startUnit 写
func (p *Player) SetStartUnit(v int) { p.data.SetStartUnit(v) }

// AiDifficulty 对应 aiDifficulty
func (p *Player) AiDifficulty() int { return p.data.GetAiDifficulty() }

// SetAiDifficulty 对应 aiDifficulty 写
func (p *Player) SetAiDifficulty(v int) { p.data.SetAiDifficulty(v) }

// Name 对应 name
func (p *Player) Name() string { return p.data.GetName() }

// ConnectHexID 对应 connectHexID
func (p *Player) ConnectHexID() string { return p.data.GetConnectHexID() }

// Survive 对应 survive
func (p *Player) Survive() bool { return p.data.GetSurvive() }

// IsAi 对应 isAi (PlayerHess 恒 false, AiPlayer 覆盖)
func (p *Player) IsAi() bool { return false }

// StatusData 对应 statusData: 六个战绩字段的键值表
func (p *Player) StatusData() map[string]int {
	s := p.data.GetStats()
	return map[string]int{
		"unitsKilled":         s.UnitsKilled,
		"buildingsKilled":     s.BuildingsKilled,
		"experimentalsKilled": s.ExperimentalsKilled,
		"unitsLost":           s.UnitsLost,
		"buildingsLost":       s.BuildingsLost,
		"experimentalsLost":   s.ExperimentalsLost,
	}
}

// PlayerInfo 对应 playerInfo
func (p *Player) PlayerInfo() string {
	con := p.Con()
	if con == nil {
		return p.Name() + " / Position: " + strconv.Itoa(p.Position()) + " / <no connection>"
	}
	return p.Name() + " / Position: " + strconv.Itoa(p.Position()) +
		" / IP: " + con.IP() + " / Use: " + con.UseConnectionAgreement() +
		" / Admin: " + strconv.FormatBool(p.IsAdmin)
}

// InfoObject 对应 infoObject
func (p *Player) InfoObject() map[string]any {
	con := p.Con()
	m := map[string]any{
		"Name":         p.Name(),
		"Position":     p.Position(),
		"ConnectHexID": p.ConnectHexID(),
		"Admin":        p.AutoAdmin,
	}
	if con != nil {
		m["IP"] = con.IP()
		m["IPCount"] = con.IPCountry()
		m["TCP/UDP"] = con.UseConnectionAgreement()
	}
	return m
}

// UpdateDate 对应 updateDate
func (p *Player) UpdateDate() { p.data.UpdateDate() }

// SendSystemMessage 对应 sendSystemMessage
func (p *Player) SendSystemMessage(text string) error {
	con := p.Con()
	if con == nil {
		return ErrPlayerNotBound
	}
	return con.SendSystemMessage(text)
}

// SendMessage 对应 sendMessage(player, text): 以 player 的名义发聊天
func (p *Player) SendMessage(from *Player, text string) error {
	con := p.Con()
	if con == nil {
		return ErrPlayerNotBound
	}
	return con.SendChatMessage(text, from.Name(), from.Team())
}

// SendPopUps 对应 sendPopUps(msg, run): 调用 RELAY 输入框 (一次性回调)
func (p *Player) SendPopUps(msg string, run func(string)) error {
	con := p.Con()
	if con == nil {
		return ErrPlayerNotBound
	}
	con.SendRelayServerType(msg, run)
	return nil
}

// KickPlayer 对应 kickPlayer(text, time)
//
// time 为踢出时长 (秒); 0 表示只踢不禁止重进. 上游 con 为 nil 时直接返回.
// AiPlayer 的"先自移除"覆盖语义经 RemoveOnKick 钩子表达 (见字段注释).
func (p *Player) KickPlayer(text string, timeSec int) {
	con := p.Con()
	if con == nil {
		return
	}
	if p.RemoveOnKick != nil {
		p.RemoveOnKick()
	}
	p.mu.Lock()
	p.KickTime = timeutil.GetTimeFutureMillis(int64(timeSec) * 1000)
	p.mu.Unlock()
	if err := con.SendKick(text); err != nil {
		log.Errorf("[Player]", "kick send: {0}", err)
	}
}

// GetInput 对应 getinput(input, params)
func (p *Player) GetInput(key string, params ...any) string {
	if p.i18n == nil {
		return key
	}
	return p.i18n(key, params...)
}

// AddData 对应 addData
func (p *Player) AddData(name string, value any) {
	p.customMu.Lock()
	p.customData[name] = value
	p.customMu.Unlock()
}

// GetData 对应 getData(name): 不存在返回 (零值, false)
func (p *Player) GetData(name string) (any, bool) {
	p.customMu.Lock()
	defer p.customMu.Unlock()
	v, ok := p.customData[name]
	return v, ok
}

// GetDataDefault 对应 getData(name, defValue)
func (p *Player) GetDataDefault(name string, def any) any {
	if v, ok := p.GetData(name); ok {
		return v
	}
	return def
}

// RemoveData 对应 removeData
func (p *Player) RemoveData(name string) {
	p.customMu.Lock()
	delete(p.customData, name)
	p.customMu.Unlock()
}

// PlayerJumpsToAnotherServer 对应 playerJumpsToAnotherServer(ip, port)
//
// 上游: 校验域名 -> 校验连接存活 -> 发送跳转包. 默认端口 5123 与上游一致.
// 跳转包的构造与发送委托给连接实现 (见 PlayerConnect.SendJumpToAnotherServer).
func (p *Player) PlayerJumpsToAnotherServer(ip string, port int) error {
	if !isutils.IsDomainName(ip) {
		return ErrNetDomain
	}
	con := p.Con()
	if con == nil {
		return ErrNetConnectClosed
	}
	con.SendJumpToAnotherServer(ip + ":" + strconv.Itoa(port))
	return nil
}

// Clear 对应 clear: 解除连接绑定
func (p *Player) Clear() {
	p.mu.Lock()
	p.con = nil
	p.mu.Unlock()
}

// Equals 对应 equals: 以 connectHexID 判定同一玩家
//
// 上游还接受"对方是字符串"的比较 (connectHexID == other.toString()),
// Go 里以 string 参数单独提供 EqualsHexID.
func (p *Player) Equals(other *Player) bool {
	if p == other {
		return true
	}
	if other == nil {
		return false
	}
	return p.ConnectHexID() == other.ConnectHexID()
}

// EqualsHexID 对应 equals 里 "other 是字符串" 的分支
func (p *Player) EqualsHexID(hexID string) bool { return p.ConnectHexID() == hexID }

// HashCode 对应 hashCode: Objects.hash(connectHexID)
func (p *Player) HashCode() int { return int(javastr.HashCode(p.ConnectHexID())) }
