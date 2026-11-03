package gameserver

import (
	"strings"
	"testing"

	"rwhps/internal/data/global"
	"rwhps/internal/game/player"
	gameio "rwhps/internal/io"
	"rwhps/internal/io/packettype"
	"rwhps/internal/util/control"
)

// testVoter 记录 ToVote 调用
type testVoter struct {
	calls int
	last  string
}

func (v *testVoter) ToVote(p *player.Player, pick string) {
	v.calls++
	v.last = pick
}

// TestReceiveChatVote 聊天消息先交给全局投票
func TestReceiveChatVote(t *testing.T) {
	g, _ := newTestServer(t, &mockHelper{})
	bindTestPlayer(t, g, "hex-v", "voter")

	v := &testVoter{}
	global.Vote = v
	t.Cleanup(func() { global.Vote = nil })

	o := gameio.NewGameOutputStream()
	o.WriteString("y")
	g.ReceiveChat(o.CreatePacket(packettype.CHAT_RECEIVE))

	if v.calls != 1 || v.last != "y" {
		t.Fatalf("投票应收到消息, calls=%d last=%q", v.calls, v.last)
	}
}

// TestReceiveChatFilterDrop 被过滤器丢弃的消息置 STOPPED 且不转引擎
func TestReceiveChatFilterDrop(t *testing.T) {
	h := &mockHelper{}
	g, _ := newTestServer(t, h)
	bindTestPlayer(t, g, "hex-f", "filteree")

	oldCore := global.Core
	global.Core = &global.Application{Admin: global.NewAdministration()}
	t.Cleanup(func() { global.Core = oldCore })
	// 加一个"丢弃含 badword 的消息"的过滤器
	global.Core.Admin.AddChatFilter(global.ChatFilterFunc(func(_ *player.Player, message string) (string, bool) {
		if strings.Contains(message, "badword") {
			return "", false
		}
		return message, true
	}))

	o := gameio.NewGameOutputStream()
	o.WriteString("some badword here")
	p := o.CreatePacket(packettype.CHAT_RECEIVE)
	g.ReceiveChat(p)

	if p.Status != control.STOPPED {
		t.Fatal("被过滤的消息应置 STOPPED")
	}
	h.mu.Lock()
	n := len(h.packets)
	h.mu.Unlock()
	if n != 0 {
		t.Fatalf("被过滤的消息不应转引擎, 转发了 %d 个包", n)
	}
}

// TestReceiveChatFewArguments 参数不足时回用法提示并 STOPPED
func TestReceiveChatFewArguments(t *testing.T) {
	h := &mockHelper{}
	g, w := newTestServer(t, h)
	bindTestPlayer(t, g, "hex-fa", "fewer")
	base := len(w.snapshot())

	// 注册一个带必选参数的命令
	g.room.ClientHandler.RegisterWith("needarg", "<x>", "HIDE", func(_ []string, _ any) {})

	o := gameio.NewGameOutputStream()
	o.WriteString(".needarg") // 缺参数
	p := o.CreatePacket(packettype.CHAT_RECEIVE)
	g.ReceiveChat(p)

	if p.Status != control.STOPPED {
		t.Fatal("参数不足应置 STOPPED")
	}
	pkts := w.snapshot()
	if len(pkts)-base != 1 {
		t.Fatalf("应回一条用法提示, 新增 %d 个包", len(pkts)-base)
	}
	h.mu.Lock()
	n := len(h.packets)
	h.mu.Unlock()
	if n != 0 {
		t.Fatalf("参数不足不应转引擎, 转发了 %d 个包", n)
	}
}
