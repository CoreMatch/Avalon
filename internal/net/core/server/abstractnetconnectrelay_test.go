package server

import (
	"errors"
	"testing"

	"rwhps/internal/game/room"
	"rwhps/internal/io/packet"
	"rwhps/internal/net/core"
)

// 编译期确认: 接口可被满足, 且 24 个成员的签名没有自相矛盾
var (
	_ AbstractNetConnectRelay = (*fakeRelayConnect)(nil)
	_ AbstractNetConnectData  = (*fakeRelayConnect)(nil)
)

// fakeRelayConnect 同时实现 AbstractNetConnectRelay 与 AbstractNetConnectData,
// 对应原版 GameVersionRelay 的
// `AbstractNetConnect(...), AbstractNetConnectData, AbstractNetConnectRelay` 组合.
//
// 它不是 GameVersionRelay 的移植 —— 那个类的函数体还没搬 (它是下一个提交),
// 这里只是一个用来验证"接口可满足"的最小假实现.
type fakeRelayConnect struct {
	status core.RelayStatus
	r      *room.RelayRoom

	// 记录调用, 供断言
	cache      *packet.Packet
	lastSent   *packet.Packet
	info       int
	register   int
	verifySent int
	serverType []string
	serverId   []bool
	relayT4    []string
	group      int
	groupPing  int
	connect    int
	reConnect  int
	relaySend  int
	toHost     int
	disconnect int
	multicast  int

	// relayDirectInspection 记下的参数, 用于区分两条分支
	directWithRoom bool
	directCalls    int

	// sendRelayServerType 存下的回调, 供测试手动触发 (对应原版的 relaySelect)
	typeReply func(string)

	// 注入错误
	failDirect  error
	failChat    error
	failCommand error

	// 返回值
	verifyResult bool
}

func (f *fakeRelayConnect) PermissionStatus() core.RelayStatus { return f.status }
func (f *fakeRelayConnect) Room() *room.RelayRoom              { return f.r }
func (f *fakeRelayConnect) Name() string                       { return "relay-tester" }
func (f *fakeRelayConnect) RegisterPlayerId() string           { return "" }
func (f *fakeRelayConnect) BetaGameVersion() bool              { return false }
func (f *fakeRelayConnect) ClientVersion() int                 { return 151 }
func (f *fakeRelayConnect) SetCachePacket(p *packet.Packet)    { f.cache = p }
func (f *fakeRelayConnect) SetLastSentPacket(p *packet.Packet) { f.lastSent = p }

func (f *fakeRelayConnect) SendRelayServerInfo() { f.info++ }

func (f *fakeRelayConnect) RelayDirectInspection(relayRoom *room.RelayRoom) error {
	f.directCalls++
	f.directWithRoom = relayRoom != nil
	if f.failDirect != nil {
		return f.failDirect
	}
	if relayRoom != nil {
		f.r = relayRoom
	}
	return nil
}

func (f *fakeRelayConnect) RelayRegisterConnection(p *packet.Packet) { f.register++ }
func (f *fakeRelayConnect) SendVerifyClientValidity()                { f.verifySent++ }

func (f *fakeRelayConnect) ReceiveVerifyClientValidity(p *packet.Packet) bool {
	return f.verifyResult
}

func (f *fakeRelayConnect) SendRelayServerType(msg string, run func(string)) {
	f.serverType = append(f.serverType, msg)
	f.typeReply = run
}

func (f *fakeRelayConnect) SendRelayServerTypeReply(p *packet.Packet) {
	if f.typeReply != nil {
		f.typeReply("picked")
	}
}
func (f *fakeRelayConnect) SendRelayServerId(multicast bool) {
	f.serverId = append(f.serverId, multicast)
}
func (f *fakeRelayConnect) ReceiveChat(p *packet.Packet) error {
	if f.failChat != nil {
		return f.failChat
	}
	return nil
}
func (f *fakeRelayConnect) ReceiveCommand(p *packet.Packet) error {
	if f.failCommand != nil {
		return f.failCommand
	}
	return nil
}
func (f *fakeRelayConnect) RelayT4(msg string)                 { f.relayT4 = append(f.relayT4, msg) }
func (f *fakeRelayConnect) PingData(p *packet.Packet)          {}
func (f *fakeRelayConnect) AddGroup(p *packet.Packet)          { f.group++ }
func (f *fakeRelayConnect) AddGroupPing(p *packet.Packet)      { f.groupPing++ }
func (f *fakeRelayConnect) AddRelayConnect()                   { f.connect++ }
func (f *fakeRelayConnect) AddReRelayConnect()                 { f.reConnect++ }
func (f *fakeRelayConnect) AddRelaySend(p *packet.Packet)      { f.relaySend++ }
func (f *fakeRelayConnect) SendPackageToHOST(p *packet.Packet) { f.toHost++ }
func (f *fakeRelayConnect) RelayPlayerDisconnect()             { f.disconnect++ }
func (f *fakeRelayConnect) MulticastAnalysis(p *packet.Packet) { f.multicast++ }

// TestAbstractNetConnectRelayIsUsableAsSeam 验证调用方可以只依赖接口 —— 这是
// "seam"的最低要求. GameVersionRelay 移植时不需要 import 本包.
func TestAbstractNetConnectRelayIsUsableAsSeam(t *testing.T) {
	fake := &fakeRelayConnect{status: core.RelayInitialConnection}

	var relay AbstractNetConnectRelay = fake
	var data AbstractNetConnectData = fake

	// Room() 的 nil 语义: 未加入房间时必须是 nil (Kotlin 的 null)
	if relay.Room() != nil {
		t.Fatal("新连接的 Room 不应为 nil")
	}

	relay.SendRelayServerInfo()
	relay.SendVerifyClientValidity()
	relay.RelayRegisterConnection(&packet.Packet{})
	relay.AddRelayConnect()
	relay.AddReRelayConnect()
	relay.AddRelaySend(&packet.Packet{})
	relay.SendPackageToHOST(&packet.Packet{})
	relay.RelayPlayerDisconnect()
	relay.MulticastAnalysis(&packet.Packet{})
	relay.RelayT4("hello")
	relay.SendRelayServerId(false)
	relay.SendRelayServerId(true)

	if fake.info != 1 || fake.verifySent != 1 || fake.register != 1 {
		t.Errorf("info/verifySent/register = %d/%d/%d, 都应为 1", fake.info, fake.verifySent, fake.register)
	}
	if fake.connect != 1 || fake.reConnect != 1 || fake.relaySend != 1 {
		t.Errorf("connect/reConnect/relaySend = %d/%d/%d", fake.connect, fake.reConnect, fake.relaySend)
	}
	if fake.toHost != 1 || fake.disconnect != 1 || fake.multicast != 1 {
		t.Errorf("toHost/disconnect/multicast = %d/%d/%d", fake.toHost, fake.disconnect, fake.multicast)
	}
	if len(fake.relayT4) != 1 || fake.relayT4[0] != "hello" {
		t.Errorf("relayT4 = %v", fake.relayT4)
	}
	if len(fake.serverId) != 2 || fake.serverId[0] || !fake.serverId[1] {
		t.Errorf("serverId = %v, 应为 [false true]", fake.serverId)
	}

	// AbstractNetConnectData 的成员也要能通过同一个对象读到
	if data.Name() != "relay-tester" || data.ClientVersion() != 151 {
		t.Errorf("Data = (%q, %d)", data.Name(), data.ClientVersion())
	}
}

// TestRelayDirectInspectionTwoPaths 固定 relayDirectInspection 的参数分支:
// 传 nil 走"服务端类型列表"路, 传房间走"直接加入"路. Go 侧没有默认参数,
// 调用方必须显式传 nil.
func TestRelayDirectInspectionTwoPaths(t *testing.T) {
	fake := &fakeRelayConnect{}

	if err := fake.RelayDirectInspection(nil); err != nil {
		t.Fatalf("RelayDirectInspection(nil): %v", err)
	}
	if fake.directWithRoom {
		t.Fatal("传 nil 时 directWithRoom 应为 false")
	}
	if fake.Room() != nil {
		t.Fatal("传 nil 不应记录房间")
	}

	r := room.NewSingleRoom("relay-room-1", nil)
	if err := fake.RelayDirectInspection(r); err != nil {
		t.Fatalf("RelayDirectInspection(room): %v", err)
	}
	if !fake.directWithRoom {
		t.Fatal("传房间时 directWithRoom 应为 true")
	}
	if fake.Room() != r {
		t.Fatal("传房间后 Room() 应返回该房间 (原版 this.room = relayRoom)")
	}
	if fake.directCalls != 2 {
		t.Errorf("directCalls = %d, want 2", fake.directCalls)
	}
}

// TestSetCachePacketFeedsDirectInspection 固定缓存包的写入/读取闭环.
//
// 原版 relayDirectInspection 第一行就是 `GameInputStream(cachePacket!!)`,
// 所以 SetCachePacket 是它的前置条件.
func TestSetCachePacketFeedsDirectInspection(t *testing.T) {
	fake := &fakeRelayConnect{}
	if fake.cache != nil {
		t.Fatal("初始 cache 应为 nil")
	}

	p := &packet.Packet{}
	fake.SetCachePacket(p)
	if fake.cache != p {
		t.Fatal("SetCachePacket 之后 cache 应指向同一对象")
	}

	// Relay 风味下 setlastSentPacket 是空实现 (原版注释: 此协议下不被使用),
	// 但接口仍然要求它存在, 调用不应 panic.
	fake.SetLastSentPacket(p)
}

// TestSendRelayServerTypeCallbackIsOptional 固定 run 参数的可空语义.
func TestSendRelayServerTypeCallbackIsOptional(t *testing.T) {
	fake := &fakeRelayConnect{}

	// nil 回调: 存下来但不应触发
	fake.SendRelayServerType("server list", nil)
	if len(fake.serverType) != 1 || fake.serverType[0] != "server list" {
		t.Fatalf("serverType = %v", fake.serverType)
	}

	var picked string
	fake.SendRelayServerType("server list 2", func(id string) { picked = id })
	if len(fake.serverType) != 2 {
		t.Fatalf("serverType = %v", fake.serverType)
	}

	// 原版在 sendRelayServerTypeReply 里回调 relaySelect
	fake.SendRelayServerTypeReply(&packet.Packet{})
	if picked != "picked" {
		t.Errorf("回调结果 = %q, want %q", picked, "picked")
	}
}

// TestReceiveVerifyClientValidityFailureIsUndifferentiated 固定原版的一个特性:
// "没有挑战"与"挑战不匹配"都返回同一个 false, 由上层决定是否断开.
func TestReceiveVerifyClientValidityFailureIsUndifferentiated(t *testing.T) {
	if (&fakeRelayConnect{verifyResult: false}).ReceiveVerifyClientValidity(&packet.Packet{}) {
		t.Error("校验失败必须返回 false")
	}
	if !(&fakeRelayConnect{verifyResult: true}).ReceiveVerifyClientValidity(&packet.Packet{}) {
		t.Error("校验通过必须返回 true")
	}
}

// TestThrowingMembersPropagateErrors 固定三个 @Throws 成员在 Go 侧返回 error.
//
// 原版只有这三个带 @Throws(IOException::class), 其余成员在函数体内 catch 掉
// 只打日志 —— 因此 Go 侧也只有这三个有 error 返回值, 接口形状与异常声明一一对应.
func TestThrowingMembersPropagateErrors(t *testing.T) {
	boom := errors.New("stream closed")

	direct := &fakeRelayConnect{failDirect: boom}
	if err := direct.RelayDirectInspection(nil); !errors.Is(err, boom) {
		t.Errorf("RelayDirectInspection err = %v, want %v", err, boom)
	}

	chat := &fakeRelayConnect{failChat: boom}
	if err := chat.ReceiveChat(&packet.Packet{}); !errors.Is(err, boom) {
		t.Errorf("ReceiveChat err = %v, want %v", err, boom)
	}

	cmd := &fakeRelayConnect{failCommand: boom}
	if err := cmd.ReceiveCommand(&packet.Packet{}); !errors.Is(err, boom) {
		t.Errorf("ReceiveCommand err = %v, want %v", err, boom)
	}
}
