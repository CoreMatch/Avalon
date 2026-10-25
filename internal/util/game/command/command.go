// Package command 对应 net.rwhps.server.util.game.command.CommandHandler
package command

import (
	"errors"
	"fmt"
	"strings"
)

// ResponseType 命令状态
type ResponseType int

const (
	NoCommand ResponseType = iota
	UnknownCommand
	FewArguments
	ManyArguments
	Valid
)

// CommandParam 参数定义
type CommandParam struct {
	Name     string
	Optional bool
	Variadic bool
}

// Runner 命令执行器
type Runner func(args []string, param any)

// Command 命令定义
type Command struct {
	Text        string
	ParamText   string
	Description string
	Params      []CommandParam
	runner      Runner
}

// CommandResponse 命令处理结果
type CommandResponse struct {
	Type       ResponseType
	Command    *Command
	RunCommand string
}

// Handler 对应 CommandHandler
type Handler struct {
	commands        map[string]*Command
	orderedCommands []*Command
	prefix          string
}

// NewHandler 创建带前缀的命令处理器
func NewHandler(prefix string) *Handler {
	return &Handler{commands: make(map[string]*Command, 16), prefix: prefix}
}

// SetPrefix 设置前缀
func (h *Handler) SetPrefix(prefix string) { h.prefix = prefix }

// HandleMessage 处理无参消息
func (h *Handler) HandleMessage(message string) *CommandResponse {
	return h.HandleMessageWith(message, nil)
}

// HandleMessageWith 处理消息
//
// 与原版一致: 只有 "不以 prefix 开头" 才算 noCommand. 空前缀下空消息也算一条命令,
// 会走到 unknownCommand —— 之前额外判断 message == "" 并返回 noCommand, 与原版不符.
// (控制台自身在调用前会跳过空行, 所以只有 RCON 等其它入口能观察到这个差异.)
func (h *Handler) HandleMessageWith(message string, params any) *CommandResponse {
	if !strings.HasPrefix(message, h.prefix) {
		return &CommandResponse{Type: NoCommand}
	}

	message = message[len(h.prefix):]

	var commandstr, argstr string
	if idx := strings.Index(message, " "); idx >= 0 {
		commandstr = message[:idx]
		argstr = message[idx+1:]
	} else {
		commandstr = message
	}

	var result []string

	cmd, ok := h.commands[strings.ToLower(commandstr)]
	if !ok || cmd == nil {
		return &CommandResponse{Type: UnknownCommand, RunCommand: commandstr}
	}

	index := 0
	satisfied := false

	for {
		if index >= len(cmd.Params) && argstr != "" {
			return &CommandResponse{Type: ManyArguments, Command: cmd, RunCommand: commandstr}
		} else if argstr == "" {
			break
		}

		if cmd.Params[index].Optional || index >= len(cmd.Params)-1 || cmd.Params[index+1].Optional {
			satisfied = true
		}

		if cmd.Params[index].Variadic {
			result = append(result, argstr)
			break
		}

		next := strings.Index(argstr, " ")
		if next == -1 {
			if !satisfied {
				return &CommandResponse{Type: FewArguments, Command: cmd, RunCommand: commandstr}
			}
			result = append(result, argstr)
			break
		}
		arg := splitOutsideQuotesFirst(argstr)
		argstr = argstr[len(arg)+1:]
		result = append(result, strings.ReplaceAll(arg, "\"", ""))
		index++
	}

	if !satisfied && len(cmd.Params) > 0 && !cmd.Params[0].Optional {
		return &CommandResponse{Type: FewArguments, Command: cmd, RunCommand: commandstr}
	}

	cmd.runner(result, params)
	return &CommandResponse{Type: Valid, Command: cmd, RunCommand: commandstr}
}

// splitOutsideQuotesFirst 等价于 argstr.split(" (?=([^\"]*\"[^\"]*\")*[^\"]*$)")[0]
func splitOutsideQuotesFirst(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' && countQuotes(s[i+1:])%2 == 0 {
			return s[:i]
		}
	}
	return s
}

func countQuotes(s string) int {
	return strings.Count(s, "\"")
}

// RemoveCommand 删除命令
func (h *Handler) RemoveCommand(text string) {
	c, ok := h.commands[text]
	if !ok || c == nil {
		return
	}
	delete(h.commands, text)
	for i, oc := range h.orderedCommands {
		if oc == c {
			h.orderedCommands = append(h.orderedCommands[:i], h.orderedCommands[i+1:]...)
			break
		}
	}
}

// Register 注册无参命令
func (h *Handler) Register(text, description string, runner Runner) *Command {
	return h.RegisterWith(text, "", description, runner)
}

// RegisterSeq 注册无参命令 (ConsSeq 风格)
func (h *Handler) RegisterSeq(text, description string, runner func(args []string)) *Command {
	return h.Register(text, description, func(args []string, _ any) { runner(args) })
}

// RegisterWith 注册带参命令
//
// 参数声明非法时 panic: 原版 Command 构造函数抛 IllegalArgumentException,
// 属于调用方的编码错误, 应在启动阶段立刻暴露而不是静默降级.
func (h *Handler) RegisterWith(text, params, description string, runner Runner) *Command {
	cmd, err := newCommand(text, params, description, runner)
	if err != nil {
		panic(err)
	}
	h.commands[strings.ToLower(text)] = cmd
	h.orderedCommands = append(h.orderedCommands, cmd)
	return cmd
}

// RegisterWithSeq 注册带参命令 (ConsSeq 风格)
func (h *Handler) RegisterWithSeq(text, params, description string, runner func(args []string)) *Command {
	return h.RegisterWith(text, params, description, func(args []string, _ any) { runner(args) })
}

// CommandList 返回有序命令列表
func (h *Handler) CommandList() []*Command { return h.orderedCommands }

// Prefix 返回前缀
func (h *Handler) Prefix() string { return h.prefix }

// newCommand 对应 Command(text, paramText, description, runner) 构造函数
//
// 参数声明非法时返回 error (原版抛 IllegalArgumentException), 由 RegisterWith 转成 panic.
func newCommand(text, paramText, description string, runner Runner) (*Command, error) {
	cmd := &Command{Text: text, ParamText: paramText, Description: description, runner: runner}

	var psplit []string
	if paramText != "" {
		psplit = strings.Split(paramText, " ")
	}
	if len(psplit) == 0 {
		cmd.Params = []CommandParam{}
		return cmd, nil
	}

	cmd.Params = make([]CommandParam, len(psplit))
	hadOptional := false
	for i, param := range psplit {
		if len(param) <= 2 {
			return nil, fmt.Errorf("malformed param '%s'", param)
		}
		l := param[0]
		r := param[len(param)-1]
		var optional, variadic bool

		if l == '<' && r == '>' {
			if hadOptional {
				return nil, errors.New("can't have non-optional param after optional param")
			}
			optional = false
		} else if l == '[' && r == ']' {
			optional = true
		} else {
			return nil, fmt.Errorf("malformed param '%s'", param)
		}
		if optional {
			hadOptional = true
		}

		fname := param[1 : len(param)-1]
		if strings.HasSuffix(fname, "...") {
			if i != len(psplit)-1 {
				return nil, errors.New("a variadic parameter should be the last parameter")
			}
			fname = fname[:len(fname)-3]
			variadic = true
		}
		cmd.Params[i] = CommandParam{Name: fname, Optional: optional, Variadic: variadic}
	}
	return cmd, nil
}
