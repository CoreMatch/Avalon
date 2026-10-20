package net

import (
	"bytes"
	"errors"
	"sync"

	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/net/core"
	"rwhps/internal/util/log"
)

// RCON 鉴权标记的 Channel 属性键 (对应 Netty AttributeKey.valueOf<Boolean>("R-CON Authenticated"))
//
// 通过将已认证标记存放在 *core.Channel 自定义属性上, 多连接场景相互独立.
const rconAttrKey = "_rcon_authenticated"

// StartRemoteControl 对应 net.rwhps.server.net.handler.tcp.StartRemoteControl
// 接管嗅探为 RCON 协议的连接:
//   - 客户端发 SERVERDATA_AUTH (type=3) 进行认证
//   - 服务端回 SERVERDATA_RESPONSE_VALUE (type=0) 作为同步 ack
//   - 校验密码:
//
//	正确 → 回 SERVERDATA_AUTH_RESPONSE(id=请求id, type=2) + 置认证标记
//	错误 → *** 行为修正 (原版 Kotlin 把任意认证请求都置为已认证) ***:
//	      回 SERVERDATA_AUTH_RESPONSE(id=-1, type=2) + 直接断开连接, 不置认证标记
//
//   - 服务器命令通过 SERVERDATA_EXECCOMMAND (type=2) 下发, body 为一行命令
type StartRemoteControl struct {
	rwHps        core.IRwHps
	rconPasswd   func() string
	cmdHandler   func(cmd string)
	connectionWg sync.WaitGroup
}

// NewStartRemoteControl 对应 StartRemoteControl(rwHps)
func NewStartRemoteControl(rwHps core.IRwHps) *StartRemoteControl {
	return &StartRemoteControl{
		rwHps:      rwHps,
		rconPasswd: func() string { return "" },
		cmdHandler: func(cmd string) {},
	}
}

// SetPasswdProvider 设置密码提供器 (Kotlin 直接读 Data.config.rconPasswd)
func (s *StartRemoteControl) SetPasswdProvider(p func() string) { s.rconPasswd = p }

// SetCommandHandler 设置命令分发器 (Kotlin 直接调 Data.SERVER_COMMAND.handleMessage)
func (s *StartRemoteControl) SetCommandHandler(h func(cmd string)) { s.cmdHandler = h }

// IsRconPacket 嗅探首字节: RCON 包以 length LE 开头, 故长度值应小于等于 4096 才像 RCON
// 用作协议判别
func (s *StartRemoteControl) IsRconPacket(firstBytes []byte) bool {
	if len(firstBytes) < 4 {
		return false
	}
	// 粗略: 包 length 必须 < 4096 才像 RCON
	_ = firstBytes[0]
	return true
}

// HandleChannel 在嗅探为 RCON 时调用, 在独立 goroutine 中处理请求循环
func (s *StartRemoteControl) HandleChannel(ch *core.Channel) {
	s.connectionWg.Add(1)
	defer s.connectionWg.Done()
	defer func() {
		_ = ch.Close()
	}()

	authenticated := false

	for {
		p, err := RconReadPacket(ch.Conn)
		if err != nil {
			if !errors.Is(err, ErrRconLengthOutOfRange) {
				return
			}
			// 非 RCON 帧 — 礼貌地断开
			return
		}

		// SERVERDATA_AUTH
		if p.Type == int(packet.RconServerdataAuth) {
			// 已认证或 body 为空 → 拒绝
			if authenticated || p.Body == "" {
				log.Warn("[RCON] 空密码或已认证请求, 拒绝")
				_ = ch.Close()
				return
			}
			// 同步 ack: SERVERDATA_RESPONSE_VALUE (type=0), 空 body
			if err := s.writeRcon(ch, packet.NewRconPacket(p.ID, int(packet.RconServerdataResponseValue), "")); err != nil {
				return
			}
			// 校验密码
			if p.Body == s.rconPasswd() {
				if err := s.writeRcon(ch, packet.NewRconPacket(p.ID, int(packet.RconServerdataAuthResponse), "")); err != nil {
					return
				}
				authenticated = true
				_ = authenticated // 占位 (后续可挂到 Channel 属性供业务使用)
			} else {
				// *** 修正: 密码错误时回 id=-1 + 直接断开, 不置认证标记 ***
				if err := s.writeRcon(ch, packet.NewRconPacket(-1, int(packet.RconServerdataAuthResponse), "")); err != nil {
					return
				}
				log.Warn("[RCON] 密码错误, 已断开连接")
				return
			}
			continue
		}

		// 已认证才允许执行命令
		if !authenticated {
			log.Warn("[RCON] 未认证, 拒绝执行命令")
			_ = ch.Close()
			return
		}

		// SERVERDATA_EXECCOMMAND
		if p.Type == int(packet.RconServerdataExecCommand) {
			s.cmdHandler(p.Body)
			// 回执: SERVERDATA_RESPONSE_VALUE, body 为空字符串 (与原版一致)
			if err := s.writeRcon(ch, packet.NewRconPacket(p.ID, int(packet.RconServerdataResponseValue), "")); err != nil {
				return
			}
			continue
		}

		// 未知类型 → 静默忽略
	}
}

// writeRcon 同步写出一条 RCON 包, 与原版 Netty.writeAndFlush 等价
func (s *StartRemoteControl) writeRcon(ch *core.Channel, p *packet.RconPacket) error {
	return ch.WriteRaw(RconWritePacket(p))
}

// WaitForConnections 等待所有活跃连接退出 (测试与优雅关闭使用)
func (s *StartRemoteControl) WaitForConnections() { s.connectionWg.Wait() }

// 编译期引用 gameio, 以备未来扩展
var _ = bytes.NewBuffer
var _ = gameio.ErrEOF