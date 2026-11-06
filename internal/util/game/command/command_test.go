package command

import (
	"strings"
	"testing"
)

// TestHandleMessageEmptyWithEmptyPrefix 空前缀下空消息算未知命令 (原版无 message==null 之外的短路)
func TestHandleMessageEmptyWithEmptyPrefix(t *testing.T) {
	h := NewHandler("")

	resp := h.HandleMessage("")
	if resp.Type != UnknownCommand {
		t.Fatalf("Type = %v, want UnknownCommand", resp.Type)
	}
	if resp.RunCommand != "" {
		t.Fatalf("RunCommand = %q, want 空串", resp.RunCommand)
	}
}

// TestHandleMessageWithoutPrefix 不以 prefix 开头才是 noCommand
func TestHandleMessageWithoutPrefix(t *testing.T) {
	h := NewHandler("/")

	if resp := h.HandleMessage("help"); resp.Type != NoCommand {
		t.Fatalf("Type = %v, want NoCommand", resp.Type)
	}
	if resp := h.HandleMessage("/help"); resp.Type == NoCommand {
		t.Fatal("带前缀的消息不应是 NoCommand")
	}
}

// TestRegisterMalformedParamPanics 参数声明非法时必须立刻失败, 而不是静默降级
func TestRegisterMalformedParamPanics(t *testing.T) {
	cases := map[string]string{
		"太短":       "<>",
		"缺少括号":     "abc",
		"非可选在可选之后": "[a] <b>",
		"变参不在最后":   "<a...> <b>",
	}
	for name, paramText := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("%q 应 panic", paramText)
				}
			}()
			NewHandler("").RegisterWith("x", paramText, "", func([]string, any) {})
		})
	}
}

// TestParamsParsed 参数名/可选/变参解析
func TestParamsParsed(t *testing.T) {
	h := NewHandler("")
	c := h.RegisterWith("startnetservice", "<id> <isPort> [sPort] [ePort]", "HIDE", func([]string, any) {})

	if len(c.Params) != 4 {
		t.Fatalf("参数数 = %d, want 4", len(c.Params))
	}
	want := []CommandParam{
		{Name: "id", Optional: false},
		{Name: "isPort", Optional: false},
		{Name: "sPort", Optional: true},
		{Name: "ePort", Optional: true},
	}
	for i, w := range want {
		if c.Params[i] != w {
			t.Fatalf("Params[%d] = %+v, want %+v", i, c.Params[i], w)
		}
	}
}

// TestVariadicConsumesRest 变参吃掉剩余全部内容
func TestVariadicConsumesRest(t *testing.T) {
	h := NewHandler("")
	var got []string
	h.RegisterWith("plugin", "<TEXT...>", "", func(args []string, _ any) { got = args })

	h.HandleMessage("plugin list all")

	if len(got) != 1 || got[0] != "list all" {
		t.Fatalf("args = %q", got)
	}
}

// TestQuotedArgument 引号内的空格不切分, 且引号被去掉
func TestQuotedArgument(t *testing.T) {
	h := NewHandler("")
	var got []string
	h.RegisterWith("say", "<a> <b>", "", func(args []string, _ any) { got = args })

	h.HandleMessage(`say "hello world" tail`)

	if len(got) != 2 || got[0] != "hello world" || got[1] != "tail" {
		t.Fatalf("args = %q", got)
	}
}

// TestFewArguments 参数不足
func TestFewArguments(t *testing.T) {
	h := NewHandler("")
	h.RegisterWith("x", "<a> <b>", "", func([]string, any) {})

	if resp := h.HandleMessage("x only"); resp.Type != FewArguments {
		t.Fatalf("Type = %v, want FewArguments", resp.Type)
	}
}

// TestManyArguments 参数过多
func TestManyArguments(t *testing.T) {
	h := NewHandler("")
	h.RegisterWith("x", "<a>", "", func([]string, any) {})

	if resp := h.HandleMessage("x a b"); resp.Type != ManyArguments {
		t.Fatalf("Type = %v, want ManyArguments", resp.Type)
	}
}

// TestOptionalAllowsOmit 可选参数可以省略
func TestOptionalAllowsOmit(t *testing.T) {
	h := NewHandler("")
	var got []string
	h.RegisterWith("x", "<a> [b]", "", func(args []string, _ any) { got = args })

	if resp := h.HandleMessage("x 1"); resp.Type != Valid {
		t.Fatalf("Type = %v, want Valid", resp.Type)
	}
	if len(got) != 1 || got[0] != "1" {
		t.Fatalf("args = %q", got)
	}
}

// TestCommandTextCaseInsensitive 命令名大小写不敏感
func TestCommandTextCaseInsensitive(t *testing.T) {
	h := NewHandler("")
	called := false
	h.Register("help", "", func([]string, any) { called = true })

	h.HandleMessage("HELP")

	if !called {
		t.Fatal("命令名应大小写不敏感")
	}
}

// TestRemoveCommand 删除命令同时从有序列表移除
func TestRemoveCommand(t *testing.T) {
	h := NewHandler("")
	h.Register("a", "", func([]string, any) {})
	h.Register("b", "", func([]string, any) {})

	h.RemoveCommand("a")

	if resp := h.HandleMessage("a"); resp.Type != UnknownCommand {
		t.Fatalf("Type = %v, want UnknownCommand", resp.Type)
	}
	if len(h.CommandList()) != 1 || h.CommandList()[0].Text != "b" {
		t.Fatalf("CommandList = %v", h.CommandList())
	}
}

// TestHandlerPrefix 前缀可改
func TestHandlerPrefix(t *testing.T) {
	h := NewHandler("")
	h.SetPrefix("!")

	if resp := h.HandleMessage("help"); resp.Type != NoCommand {
		t.Fatalf("Type = %v, want NoCommand", resp.Type)
	}
	if resp := h.HandleMessage("!help"); resp.Type == NoCommand {
		t.Fatal("改前缀后 !help 不应是 NoCommand")
	}
	if got := h.Prefix(); !strings.EqualFold(got, "!") {
		t.Fatalf("Prefix() = %q", got)
	}
}
