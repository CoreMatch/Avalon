package external

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ProtocolVersion 线协议版本
//
// helper 与 Go 侧各有一份; 握手时不一致就直接失败, 不做"尽力兼容".
// 升级协议时递增这个值, 旧 helper 会得到明确错误而不是静默错位.
const ProtocolVersion = 1

// 操作名
const (
	// OpHello 握手: helper 报告自己支持的协议版本
	OpHello = "hello"
	// OpStart 开局 (对应 ServerRoom.roomStartGame)
	OpStart = "start"
	// OpStop 结束当前局 (对应 ServerRoom.gr)
	OpStop = "stop"
	// OpStatus 查询运行状态与人数
	OpStatus = "status"
)

// 协议层错误
var (
	// ErrProtocol 线协议本身出错: 非法 JSON / 版本不匹配 / id 不对应 / 空行
	ErrProtocol = errors.New("external: protocol error")
	// ErrNotRunning helper 进程已退出 (管道 EOF 或退出码非 0)
	ErrNotRunning = errors.New("external: helper process is not running")
)

// Request 是 Go -> helper 的一条消息 (一行 JSON)
type Request struct {
	// V 协议版本
	V int `json:"v"`
	// ID 请求序号, 与 Response.ID 对应; 由 Go 侧单调递增分配
	ID int64 `json:"id"`
	// Op 操作名
	Op string `json:"op"`

	// Room 对应 MatchConfig.RoomID
	Room string `json:"room,omitempty"`
	// Map 对应 MatchConfig.MapName
	Map string `json:"map,omitempty"`
	// MapPlayer 对应 MatchConfig.MapPlayer
	MapPlayer string `json:"mapPlayer,omitempty"`
	// MaxPlayer 对应 MatchConfig.MaxPlayer
	MaxPlayer int `json:"maxPlayer,omitempty"`
}

// Response 是 helper -> Go 的一条消息 (一行 JSON)
type Response struct {
	// V 协议版本
	V int `json:"v"`
	// ID 对应 Request.ID
	ID int64 `json:"id"`
	// OK 操作是否成功
	OK bool `json:"ok"`
	// Error OK 为 false 时的原因, 原样透传给上层日志
	Error string `json:"error,omitempty"`

	// Running 仅 status 有效: 是否正在对局中
	Running bool `json:"running,omitempty"`
	// Players 仅 status 有效: 当前人数
	Players int `json:"players,omitempty"`
}

// encodeRequest 把请求序列化成一行 (不含换行符)
//
// 必须关掉 HTML 转义: 地图名里完全可能出现 < > & (例如 "[z;p10] <team>"),
// 默认行为会把它们变成 \u003c 之类的序列, helper 侧就收到了另一个字符串.
//
// 这里只能用 json.Encoder + SetEscapeHTML(false): json.Marshal 没有关掉的开关,
// 它恒定转义 HTML 字符; Encoder 会多写一个 '\n', 由调用方 trim 掉.
func encodeRequest(req *Request) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(req); err != nil {
		return "", fmt.Errorf("%w: encode request: %v", ErrProtocol, err)
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// decodeResponse 解析 helper 的一行输出
//
// 与原版的对应关系: 这里替代的是 Kotlin 侧 ASM 改写后直接调 java 对象的那条路.
// 所有字段缺失都按零值处理, 由调用方判断.
func decodeResponse(line string) (*Response, error) {
	if line == "" {
		return nil, fmt.Errorf("%w: empty line", ErrProtocol)
	}

	var resp Response
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, fmt.Errorf("%w: decode response %q: %v", ErrProtocol, line, err)
	}
	if resp.V != ProtocolVersion {
		return nil, fmt.Errorf("%w: helper speaks protocol %d, Go side is %d", ErrProtocol, resp.V, ProtocolVersion)
	}
	return &resp, nil
}

// errorf 把 helper 返回的失败原因包装成 error
//
// helper 的 Error 字段原样保留, 不做任何加工 —— 它比 Go 侧更能说清发生了什么.
func errorf(resp *Response) error {
	if resp == nil || resp.OK {
		return nil
	}
	if resp.Error == "" {
		return errors.New("external: helper reported failure without a reason")
	}
	return errors.New("external: helper reported failure: " + resp.Error)
}
