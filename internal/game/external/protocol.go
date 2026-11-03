package external

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ProtocolVersion 线协议版本
//
// helper 与 Go 侧各有一份; 握手时不一致就直接失败, 不做"尽力兼容".
// 升级协议时递增这个值, 旧 helper 会得到明确错误而不是静默错位.
//
// v2 相对 v1 的变化:
//   - 引入统一的消息信封 (type 判别), 响应与主动消息可以在同一条流上共存.
//   - 房间 ID / 连接 ID 成为一等维度 (一 helper 多房间).
//   - 游戏包负载走 Base64, 保留二进制安全.
//   - 增加玩家档案、聊天与命令转发、致命错误三类主动消息.
//   - 每一条消息都带世代号 (gen), 用于识别"上一代 helper 迟到的消息".
//
// 线格式: 每行一条 JSON, LF 结尾, UTF-8.
//
//	Go -> helper (请求):
//	  {"v":2,"id":1,"op":"room.start","room":"r1","map":"...","gen":1}
//
//	helper -> Go (应答/主动消息):
//	  {"v":2,"type":"response","id":1,"ok":true,"running":true,"players":3,"gen":1}
//	  {"v":2,"type":"packet","room":"r1","conn":"c1","ptype":115,"data":"<base64>","gen":1}
//	  {"v":2,"type":"event","event":"chat","room":"r1","data":{...},"gen":1}
//	  {"v":2,"type":"fatal","error":"...","gen":1}
//
// 包负载编码: 标准 Base64 (RFC 4648, 带填充), 与 java.util.Base64.getEncoder() 一致.
//
// 世代号 (gen) 规则:
//
//	Go 侧每次拉起一个 helper 进程就递增一次世代号, 从 1 开始.
//	helper 在握手 (hello) 时学到自己的世代号, 此后每一条消息都回带它.
//	Go 侧收到世代号不等于当前世代的消息一律丢弃 —— 上一代 helper 在管道里
//	迟到的包如果混进来, 会被发给新一轮对局的客户端, 表现为"客户端莫名乱动".
const ProtocolVersion = 2

// 消息类型 (helper -> Go)
const (
	// MsgResponse 对某次请求的应答, 用 ID 与请求对齐
	MsgResponse = "response"
	// MsgPacket 游戏引擎产生、需要转发给客户端的游戏包
	MsgPacket = "packet"
	// MsgEvent 玩家档案 / 聊天 / 命令 / 房间状态等事件
	MsgEvent = "event"
	// MsgFatal helper 或其子进程的致命错误
	MsgFatal = "fatal"
)

// 操作名 (Go -> helper)
const (
	// OpHello 握手: 校验协议版本, 返回 helper 状态
	OpHello = "hello"
	// OpRoomCreate 创建一个房间 (对应一个独立 ClassLoader 与游戏引擎实例)
	//
	// 幂等: 对已存在的房间再次创建必须返回 OK 并保持原房间.
	// Go 侧按世代跟踪房间避免重复创建, 但 helper 崩溃重启等时序下
	// 仍可能收到重复创建, 拒绝会导致开局链路整体失败.
	OpRoomCreate = "room.create"
	// OpRoomDestroy 销毁一个房间
	OpRoomDestroy = "room.destroy"
	// OpRoomStart 开局 (对应 ServerRoom.roomStartGame)
	OpRoomStart = "room.start"
	// OpRoomStop 结束当前局 (对应 ServerRoom.gr)
	OpRoomStop = "room.stop"
	// OpRoomStatus 查询房间运行状态与人数
	OpRoomStatus = "room.status"
	// OpConnOpen 登记一条新的客户端连接
	OpConnOpen = "conn.open"
	// OpConnClose 关闭一条客户端连接
	OpConnClose = "conn.close"
	// OpConnPacket 把客户端发来的游戏包转给游戏引擎
	OpConnPacket = "conn.packet"
)

// 事件名 (helper -> Go, 出现在 MsgEvent 的 event 字段)
const (
	// EventPlayerJoin 玩家进入房间
	EventPlayerJoin = "player.join"
	// EventPlayerLeave 玩家离开房间
	EventPlayerLeave = "player.leave"
	// EventPlayerUpdate 玩家档案变化 (队伍 / 准备状态等)
	EventPlayerUpdate = "player.update"
	// EventChat 聊天消息
	EventChat = "chat"
	// EventCommand 游戏命令
	EventCommand = "command"
	// EventRoomStarted 房间开局
	EventRoomStarted = "room.started"
	// EventRoomStopped 房间结束本局
	EventRoomStopped = "room.stopped"
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
	// Gen 世代号, Go 侧在每次拉起新进程时递增
	//
	// 它的作用是让 helper 能识别"这条请求属于哪一代 helper":
	// 崩溃重启后旧连接上的包必须被丢弃, 否则会串到新一局的房间里.
	//
	// 世代号从 1 开始 (0 表示"还没建立"), 不使用 omitempty:
	// 协议里每一行都必须带着自己的世代号, 否则无法区分"旧进程残留的迟到消息".
	Gen uint64 `json:"gen"`

	// Room 房间 ID (一 helper 多房间下的主维度)
	Room string `json:"room,omitempty"`
	// Conn 连接 ID (房间内唯一)
	Conn string `json:"conn,omitempty"`

	// Map 对应 MatchConfig.MapName
	Map string `json:"map,omitempty"`
	// MapPlayer 对应 MatchConfig.MapPlayer
	MapPlayer string `json:"mapPlayer,omitempty"`
	// MaxPlayer 对应 MatchConfig.MaxPlayer
	MaxPlayer int `json:"maxPlayer,omitempty"`

	// PType 游戏包类型号 (OpConnPacket 用)
	PType int `json:"ptype,omitempty"`
	// Data 游戏包负载, 标准 Base64 (OpConnPacket 用)
	Data string `json:"data,omitempty"`
}

// Response 是 helper 对一次请求的应答 (一行 JSON)
type Response struct {
	// V 协议版本
	V int `json:"v"`
	// Type 固定为 MsgResponse
	Type string `json:"type"`
	// Gen helper 世代号, 回显请求里的值
	//
	// Go 侧用它把应答和"发这条请求时的那一代 helper"绑定:
	// 进程重启后, 旧管道里迟到的应答不会误配到新进程上.
	Gen uint64 `json:"gen"`
	// ID 对应 Request.ID
	ID int64 `json:"id"`
	// OK 操作是否成功
	OK bool `json:"ok"`
	// Error OK 为 false 时的原因, 原样透传给上层日志
	Error string `json:"error,omitempty"`

	// Running 仅 room.status 有效: 是否正在对局中
	Running bool `json:"running,omitempty"`
	// Players 仅 room.status 有效: 当前人数
	Players int `json:"players,omitempty"`
	// Rooms 仅 hello 有效: helper 当前持有的房间数
	Rooms int `json:"rooms,omitempty"`
}

// PacketMessage 是 helper 主动发来的游戏包 (一行 JSON)
type PacketMessage struct {
	// V 协议版本
	V int `json:"v"`
	// Type 固定为 MsgPacket
	Type string `json:"type"`
	// Gen helper 世代号
	Gen uint64 `json:"gen"`
	// Room 房间 ID
	Room string `json:"room"`
	// Conn 连接 ID: 该包要发给哪条客户端连接
	Conn string `json:"conn"`
	// PType 游戏包类型号
	PType int `json:"ptype"`
	// Data 游戏包负载, 标准 Base64
	Data string `json:"data"`
}

// EventMessage 是 helper 主动发来的事件 (一行 JSON)
type EventMessage struct {
	// V 协议版本
	V int `json:"v"`
	// Type 固定为 MsgEvent
	Type string `json:"type"`
	// Gen helper 世代号
	Gen uint64 `json:"gen"`
	// Event 事件名, 见 Event* 常量
	Event string `json:"event"`
	// Room 房间 ID
	Room string `json:"room,omitempty"`
	// Conn 关联的连接 ID (玩家事件有, 房间事件没有)
	Conn string `json:"conn,omitempty"`
	// Data 事件负载, 形状由事件名决定
	Data json.RawMessage `json:"data,omitempty"`
}

// FatalMessage 是 helper 报出的致命错误 (一行 JSON)
type FatalMessage struct {
	// V 协议版本
	V int `json:"v"`
	// Type 固定为 MsgFatal
	Type string `json:"type"`
	// Gen helper 世代号
	Gen uint64 `json:"gen"`
	// Error 错误描述
	Error string `json:"error"`
	// Room 出错时的房间 ID (如果有)
	Room string `json:"room,omitempty"`
}

// PlayerProfile 是 Go 侧镜像的玩家完整档案
//
// 里程碑 1 里 Java 仍是权威方, 但协议必须把这些字段完整转发出来,
// 否则 Go 侧永远只能显示一个空房间.
type PlayerProfile struct {
	// ID 玩家在游戏内的唯一标识
	ID string `json:"id"`
	// Name 玩家名字
	Name string `json:"name"`
	// Team 队伍号
	Team int `json:"team"`
	// Ready 是否已准备
	Ready bool `json:"ready"`
	// Host 是否是房主
	Host bool `json:"host"`
	// Bot 是否是电脑玩家
	Bot bool `json:"bot"`
	// Spectator 是否是观战者
	Spectator bool `json:"spectator"`
}

// ChatPayload 是 EventChat 的负载
type ChatPayload struct {
	// Name 发言者名字
	Name string `json:"name"`
	// Text 发言内容
	Text string `json:"text"`
}

// CommandPayload 是 EventCommand 的负载
type CommandPayload struct {
	// Name 发起者名字
	Name string `json:"name"`
	// Text 命令原文
	Text string `json:"text"`
}

// header 是所有 helper -> Go 消息共有的判别头
type header struct {
	V    int    `json:"v"`
	Type string `json:"type"`
	Gen  uint64 `json:"gen"`
}

// encodeRequest 把请求序列化成一行 (不含换行符)
//
// 必须关掉 HTML 转义: 地图名里完全可能出现 < > & (例如 "[z;p10] <team>"),
// 默认行为会把它们变成 之类的序列, helper 侧就收到了另一个字符串.
//
// 这里只能用 json.Encoder + SetEscapeHTML(false): json.Marshal 没有关掉的开关,
// 它恒定转义 HTML 字符; Encoder 会多写一个 '\n', 由调用方 trim 掉.
func encodeRequest(req *Request) (string, error) {
	if err := validateRequest(req); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(req); err != nil {
		return "", fmt.Errorf("%w: encode request: %v", ErrProtocol, err)
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// validateRequest 校验请求自身的字段完整性
//
// 放在编码之前而不是让 helper 去拒绝, 是因为协议错位是"两侧版本不一致"的表现:
// helper 收到一个字段缺失的请求, 它没法区分"Go 版没发这个字段"和"Go 版发错了字段",
// 只能各自脑补. 在 Go 侧就把请求拦下来, 错误信息能直接指向发起方.
//
// 按操作校验必填字段:
//   - hello: 不带任何房间/连接字段
//   - room.*: 必须带 room
//   - conn.*: 必须带 room 与 conn
//   - conn.packet: 另外必须带 ptype
func validateRequest(req *Request) error {
	if req == nil {
		return fmt.Errorf("%w: nil request", ErrProtocol)
	}
	if req.V != ProtocolVersion {
		return fmt.Errorf("%w: request speaks protocol %d, Go side is %d", ErrProtocol, req.V, ProtocolVersion)
	}
	if req.ID <= 0 {
		return fmt.Errorf("%w: request id must be positive, got %d", ErrProtocol, req.ID)
	}
	if req.Gen == 0 {
		return fmt.Errorf("%w: request without generation", ErrProtocol)
	}

	switch req.Op {
	case OpHello:
		// 握手不带任何房间/连接字段: helper 收到一个带 room 的 hello 时
		// 无法判断对方是把房间参数塞错了地方, 还是协议版本不同.
		if req.Room != "" || req.Conn != "" {
			return fmt.Errorf("%w: hello must not carry room or conn", ErrProtocol)
		}
		return nil
	case OpRoomCreate, OpRoomDestroy, OpRoomStart, OpRoomStop, OpRoomStatus:
		if req.Room == "" {
			return fmt.Errorf("%w: %s without room", ErrProtocol, req.Op)
		}
		return nil
	case OpConnOpen, OpConnClose:
		if req.Room == "" {
			return fmt.Errorf("%w: %s without room", ErrProtocol, req.Op)
		}
		if req.Conn == "" {
			return fmt.Errorf("%w: %s without conn", ErrProtocol, req.Op)
		}
		return nil
	case OpConnPacket:
		if req.Room == "" {
			return fmt.Errorf("%w: %s without room", ErrProtocol, req.Op)
		}
		if req.Conn == "" {
			return fmt.Errorf("%w: %s without conn", ErrProtocol, req.Op)
		}
		if req.PType <= 0 {
			return fmt.Errorf("%w: %s without packet type", ErrProtocol, req.Op)
		}
		return nil
	case "":
		return fmt.Errorf("%w: request without op", ErrProtocol)
	default:
		return fmt.Errorf("%w: unknown op %q", ErrProtocol, req.Op)
	}
}

// decodeResponse 解析一条 response 消息
//
// 与原版的对应关系: 这里替代的是 Kotlin 侧 ASM 改写后直接调 java 对象的那条路.
// 所有字段缺失都按零值处理, 由调用方判断.
func decodeResponse(line string) (*Response, error) {
	msg, err := decodeMessage(line)
	if err != nil {
		return nil, err
	}
	resp, ok := msg.(*Response)
	if !ok {
		return nil, fmt.Errorf("%w: expected %s, got %T", ErrProtocol, MsgResponse, msg)
	}
	return resp, nil
}

// decodeMessage 解析 helper 发来的任意一条消息
//
// 返回的具体类型由 type 判别:
//   - MsgResponse -> *Response
//   - MsgPacket   -> *PacketMessage
//   - MsgEvent    -> *EventMessage
//   - MsgFatal    -> *FatalMessage
//
// 未知类型是致命错误: 静默忽略会让两侧的协议版本悄悄错位.
//
// 每种消息还会做各自的必填字段校验 (世代号 / 房间 / 连接 ID 等):
// 空字段会让上层把包路由到"未知的连接"上, 表现出来是客户端莫名其妙断线,
// 比直接报错难查得多.
func decodeMessage(line string) (any, error) {
	if line == "" {
		return nil, fmt.Errorf("%w: empty line", ErrProtocol)
	}

	var head header
	if err := json.Unmarshal([]byte(line), &head); err != nil {
		return nil, fmt.Errorf("%w: decode message %q: %v", ErrProtocol, line, err)
	}
	if head.V != ProtocolVersion {
		return nil, fmt.Errorf("%w: helper speaks protocol %d, Go side is %d", ErrProtocol, head.V, ProtocolVersion)
	}
	if head.Gen == 0 {
		return nil, fmt.Errorf("%w: helper message without generation: %q", ErrProtocol, line)
	}

	switch head.Type {
	case MsgResponse:
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			return nil, fmt.Errorf("%w: decode response %q: %v", ErrProtocol, line, err)
		}
		if resp.ID <= 0 {
			return nil, fmt.Errorf("%w: response without request id: %q", ErrProtocol, line)
		}
		return &resp, nil
	case MsgPacket:
		var pkt PacketMessage
		if err := json.Unmarshal([]byte(line), &pkt); err != nil {
			return nil, fmt.Errorf("%w: decode packet %q: %v", ErrProtocol, line, err)
		}
		if pkt.Room == "" {
			return nil, fmt.Errorf("%w: packet without room: %q", ErrProtocol, line)
		}
		if pkt.Conn == "" {
			return nil, fmt.Errorf("%w: packet without conn: %q", ErrProtocol, line)
		}
		if pkt.PType <= 0 {
			return nil, fmt.Errorf("%w: packet without type: %q", ErrProtocol, line)
		}
		return &pkt, nil
	case MsgEvent:
		var ev EventMessage
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("%w: decode event %q: %v", ErrProtocol, line, err)
		}
		if ev.Event == "" {
			return nil, fmt.Errorf("%w: event without name: %q", ErrProtocol, line)
		}
		if ev.Room == "" {
			return nil, fmt.Errorf("%w: event without room: %q", ErrProtocol, line)
		}
		switch ev.Event {
		case EventPlayerJoin, EventPlayerLeave, EventPlayerUpdate, EventChat, EventCommand:
			// 玩家级事件必须能定位到具体连接, 否则 Go 侧不知道该更新谁的档案
			if ev.Conn == "" {
				return nil, fmt.Errorf("%w: %s event without conn: %q", ErrProtocol, ev.Event, line)
			}
		}
		return &ev, nil
	case MsgFatal:
		var fatal FatalMessage
		if err := json.Unmarshal([]byte(line), &fatal); err != nil {
			return nil, fmt.Errorf("%w: decode fatal %q: %v", ErrProtocol, line, err)
		}
		if fatal.Error == "" {
			return nil, fmt.Errorf("%w: fatal without reason: %q", ErrProtocol, line)
		}
		return &fatal, nil
	default:
		return nil, fmt.Errorf("%w: unknown message type %q", ErrProtocol, head.Type)
	}
}

// decodePlayerProfile 解析玩家档案负载
//
// 事件负载的形状由事件名决定, 这里把"player.* 系列"的负载单独解出来,
// 免得每个调用点都自己写一遍 json.Unmarshal.
func decodePlayerProfile(raw json.RawMessage) (*PlayerProfile, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: player event without payload", ErrProtocol)
	}
	var p PlayerProfile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("%w: decode player profile: %v", ErrProtocol, err)
	}
	return &p, nil
}

// decodeChat 解析聊天负载
func decodeChat(raw json.RawMessage) (*ChatPayload, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: chat event without payload", ErrProtocol)
	}
	var c ChatPayload
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%w: decode chat: %v", ErrProtocol, err)
	}
	return &c, nil
}

// decodeCommand 解析命令负载
func decodeCommand(raw json.RawMessage) (*CommandPayload, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: command event without payload", ErrProtocol)
	}
	var c CommandPayload
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%w: decode command: %v", ErrProtocol, err)
	}
	return &c, nil
}

// encodeBase64 把游戏包负载编码成协议里的 Base64 字段
func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// decodeBase64 解码协议里的 Base64 负载
func decodeBase64(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: decode base64: %v", ErrProtocol, err)
	}
	return data, nil
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
