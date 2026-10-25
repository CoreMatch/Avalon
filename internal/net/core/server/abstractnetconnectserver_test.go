package server

import (
	"errors"
	"testing"

	"rwhps/internal/game"
	"rwhps/internal/net/core"
)

// 编译期确认: 接口可被满足, 且成员签名没有自相矛盾
var (
	_ AbstractNetConnectServer = (*fakeServerConnect)(nil)
	_ AbstractNetConnectData   = (*fakeServerConnect)(nil)
)

// fakeServerConnect 同时实现两个接口, 对应原版 GameVersionServer 的
// `AbstractNetConnect(...), AbstractNetConnectData, AbstractNetConnectServer` 组合.
//
// 它不是 GameVersionServer 的移植 —— 那个类直接 import 了混淆后的游戏类, 按边界
// 规则不可移植. 这里只是一个用来验证"接口可满足"的最小假实现.
type fakeServerConnect struct {
	status   core.ServerStatus
	beta     bool
	verGame  string
	verInt   int
	room     *game.GameSession
	name     string
	register string
	betaGame bool
	client   int

	// 记录调用, 供断言
	system []string
	chat   []chatCall
	kick   []string

	// 注入错误, 验证调用方能拿到失败
	failSend error
}

type chatCall struct {
	msg, sendBy string
	team        int
}

func (f *fakeServerConnect) PermissionStatus() core.ServerStatus { return f.status }
func (f *fakeServerConnect) SupportedVersionBeta() bool          { return f.beta }
func (f *fakeServerConnect) SupportedVersionGame() string        { return f.verGame }
func (f *fakeServerConnect) SupportedVersionInt() int            { return f.verInt }
func (f *fakeServerConnect) Room() *game.GameSession             { return f.room }
func (f *fakeServerConnect) Name() string                        { return f.name }
func (f *fakeServerConnect) RegisterPlayerId() string            { return f.register }
func (f *fakeServerConnect) BetaGameVersion() bool               { return f.betaGame }
func (f *fakeServerConnect) ClientVersion() int                  { return f.client }

func (f *fakeServerConnect) SendSystemMessage(msg string) error {
	if f.failSend != nil {
		return f.failSend
	}
	f.system = append(f.system, msg)
	return nil
}

func (f *fakeServerConnect) SendChatMessage(msg, sendBy string, team int) error {
	if f.failSend != nil {
		return f.failSend
	}
	f.chat = append(f.chat, chatCall{msg, sendBy, team})
	return nil
}

func (f *fakeServerConnect) SendKick(reason string) error {
	if f.failSend != nil {
		return f.failSend
	}
	f.kick = append(f.kick, reason)
	return nil
}

// TestAbstractNetConnectServerIsUsableAsSeam 验证调用方可以只依赖接口,
// 不需要知道具体类型 —— 这是"seam"的最低要求.
func TestAbstractNetConnectServerIsUsableAsSeam(t *testing.T) {
	fake := &fakeServerConnect{
		status:  core.ServerInitialConnection,
		beta:    false,
		verGame: "1.15",
		verInt:  176,
		room:    game.NewGameSession("room-1", nil),
		name:    "tester",
		client:  176,
	}

	// 只拿接口, 不拿具体类型
	var srv AbstractNetConnectServer = fake
	var data AbstractNetConnectData = fake

	if srv.SupportedVersionInt() != 176 {
		t.Errorf("SupportedVersionInt = %d, want 176", srv.SupportedVersionInt())
	}
	if srv.SupportedVersionGame() != "1.15" {
		t.Errorf("SupportedVersionGame = %q, want %q", srv.SupportedVersionGame(), "1.15")
	}
	if srv.Room() == nil {
		t.Fatal("Room 不应为 nil")
	}
	if srv.Room().RoomID != "room-1" {
		t.Errorf("Room.RoomID = %q, want %q", srv.Room().RoomID, "room-1")
	}

	if err := srv.SendSystemMessage("hello"); err != nil {
		t.Fatalf("SendSystemMessage: %v", err)
	}
	if err := srv.SendChatMessage("hi", "tester", 5); err != nil {
		t.Fatalf("SendChatMessage: %v", err)
	}
	if err := srv.SendKick("bye"); err != nil {
		t.Fatalf("SendKick: %v", err)
	}

	if len(fake.system) != 1 || fake.system[0] != "hello" {
		t.Errorf("system 消息记录 = %v", fake.system)
	}
	if len(fake.chat) != 1 || fake.chat[0].sendBy != "tester" || fake.chat[0].team != 5 {
		t.Errorf("chat 记录 = %v", fake.chat)
	}
	if len(fake.kick) != 1 || fake.kick[0] != "bye" {
		t.Errorf("kick 记录 = %v", fake.kick)
	}

	// AbstractNetConnectData 的四个属性也要能通过同一个对象读到
	if data.Name() != "tester" || data.ClientVersion() != 176 {
		t.Errorf("Data = (%q, %d)", data.Name(), data.ClientVersion())
	}
}

// TestAbstractNetConnectDataNullableMapping 锁住 registerPlayerId 的空串约定.
//
// Kotlin 是 String?, 未注册时是 null. Go 侧用空串表示 —— 若将来有人改成
// *string, 这个测试会提醒他这是一处有意的取舍而不是疏漏.
func TestAbstractNetConnectDataNullableMapping(t *testing.T) {
	fake := &fakeServerConnect{name: "tester"}
	if got := fake.RegisterPlayerId(); got != "" {
		t.Errorf("未注册时 RegisterPlayerId = %q, 期望空串", got)
	}

	fake.register = "abc123"
	if got := fake.RegisterPlayerId(); got != "abc123" {
		t.Errorf("RegisterPlayerId = %q, want %q", got, "abc123")
	}
}

// TestSendFailurePropagates Go 侧让 send* 返回 error (Kotlin 内部静默 catch),
// 这里确认失败确实能传到的调用方.
func TestSendFailurePropagates(t *testing.T) {
	boom := errors.New("connection dead")
	srv := AbstractNetConnectServer(&fakeServerConnect{failSend: boom})

	if err := srv.SendSystemMessage("x"); !errors.Is(err, boom) {
		t.Errorf("SendSystemMessage err = %v, want %v", err, boom)
	}
	if err := srv.SendChatMessage("x", "y", 1); !errors.Is(err, boom) {
		t.Errorf("SendChatMessage err = %v, want %v", err, boom)
	}
	if err := srv.SendKick("z"); !errors.Is(err, boom) {
		t.Errorf("SendKick err = %v, want %v", err, boom)
	}
}
