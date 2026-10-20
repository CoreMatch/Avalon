package core

import (
	"net/url"
	"sync/atomic"

	gameio "rwhps/internal/io"
	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/temp"
	"rwhps/internal/util/game/command"
	"rwhps/internal/util/log"
	"rwhps/internal/util/timeutil"
)

// IGroupNet 对应 net.GroupNet (为避免包循环而以接口形式在 core 中声明)
type IGroupNet interface {
	AddChannel(*Channel)
	RemoveChannel(*Channel)
	AddAgreement(*ConnectionAgreement)
	RemoveAgreement(*ConnectionAgreement)
}

// LogCommand / PingCommand 对应 Data.LOG_COMMAND / Data.PING_COMMAND
// 由上层 (net 包) 在初始化时注入, 以避免 core -> data.global 的循环依赖
var (
	LogCommand  *command.Handler
	PingCommand *command.Handler
)

// TypeConnect 对应 TypeConnect 接口
// 每个 TypeConnect 绑定一个 AbstractNetConnect
type TypeConnect interface {
	// GetTypeConnect 为该连接设置包解析器
	GetTypeConnect(connectionAgreement *ConnectionAgreement) TypeConnect

	// SetData 非必要实现
	SetData(data any)

	// ProcessConnect 协议处理
	ProcessConnect(p *packet.Packet) error

	// AbstractNetConnect 获取绑定的连接
	AbstractNetConnect() *AbstractNetConnect

	// Version 获取 TypeConnect 处理版本号
	Version() string
}

// AbstractNetConnect 对应 AbstractNetConnect
// 实现基础网络信息封装, 游戏协议强制继承此类
type AbstractNetConnect struct {
	ConnectionAgreement *ConnectionAgreement

	// NumberOfRetries 尝试数
	NumberOfRetries int

	// TryBoolean 尝试状态
	TryBoolean bool

	// IsDis 是否已断开
	IsDis bool

	// ConnectReceiveData 连接接收数据
	ConnectReceiveData *temp.ConnectReceiveData

	lastReceivedTime atomic.Int64
}

// NewAbstractNetConnect 对应 AbstractNetConnect(connectionAgreement)
func NewAbstractNetConnect(connectionAgreement *ConnectionAgreement) *AbstractNetConnect {
	a := &AbstractNetConnect{
		ConnectionAgreement: connectionAgreement,
		ConnectReceiveData:  temp.NewConnectReceiveData(),
	}
	a.lastReceivedTime.Store(timeutil.ConcurrentMillis())
	return a
}

// RwHps 对应 rwHps
func (a *AbstractNetConnect) RwHps() IRwHps { return a.ConnectionAgreement.RwHps }

// IP 获取连接 IP
func (a *AbstractNetConnect) IP() string { return a.ConnectionAgreement.IP }

// IPLong24 获取连接 IP 长整型
func (a *AbstractNetConnect) IPLong24() string { return a.ConnectionAgreement.IPLong24 }

// IPCountry 获取连接 IP 归属地
func (a *AbstractNetConnect) IPCountry() string { return a.ConnectionAgreement.IPCountry }

// IPCountryAll 获取连接 IP 完整归属地
func (a *AbstractNetConnect) IPCountryAll() string { return a.ConnectionAgreement.IPCountryAll }

// Port 获取使用的本地端口
func (a *AbstractNetConnect) Port() int { return a.ConnectionAgreement.LocalPort }

// LastReceivedTime 最后接收数据包的时间
func (a *AbstractNetConnect) LastReceivedTime() int64 { return a.lastReceivedTime.Load() }

// RefreshLastReceivedTime 对应 lastReceivedTime()
func (a *AbstractNetConnect) RefreshLastReceivedTime() {
	a.lastReceivedTime.Store(timeutil.ConcurrentMillis())
	a.ConnectReceiveData.ReceiveBigPacket = false
}

// UseConnectionAgreement 获取连接协议
func (a *AbstractNetConnect) UseConnectionAgreement() string {
	return a.ConnectionAgreement.UseAgreementStr
}

// Version 协议版本号, 子类覆盖
func (a *AbstractNetConnect) Version() string { return "" }

// Disconnect 断开连接, 子类覆盖
func (a *AbstractNetConnect) Disconnect() {}

// SendPacket 发送数据包
func (a *AbstractNetConnect) SendPacket(p *packet.Packet) {
	ca := a.ConnectionAgreement
	if err := ca.Send(p); err != nil {
		a.Disconnect()
		if ca.UseAgreementStr == "UDP" {
			log.Errorf("[%s] SendError - 本消息单独出现无妨 连续多次出现请debug", ca.UseAgreementStr)
		}
	}
}

// ReceivePacket 接收数据包 (选择向下传递), 子类覆盖
func (a *AbstractNetConnect) ReceivePacket(p *packet.Packet) {}

// Debug 对应 debug(packet) 特殊开发暂不开放
func (a *AbstractNetConnect) Debug(p *packet.Packet) {
	stream := gameio.NewGameInputStreamFromPacket(p, 0)
	str, err := url.QueryUnescape(stream.ReadString())
	if err != nil {
		a.SendDebug("Error")
		return
	}
	if LogCommand != nil {
		LogCommand.HandleMessageWith(str, a)
	}
}

// SendDebug 对应 sendDebug(str)
func (a *AbstractNetConnect) SendDebug(str string) {
	o := gameio.NewGameOutputStream()
	o.WriteString(str)
	a.SendPacket(o.CreatePacket(packettype.SERVER_DEBUG))
}

// ExCommand 对应 exCommand(packet)
func (a *AbstractNetConnect) ExCommand(p *packet.Packet) bool {
	read := gameio.NewGameInputStreamFromPacket(p, 0)
	switch packettype.PacketType(p.Type.TypeInt()) {
	case packettype.SERVER_DEBUG_RECEIVE:
		a.Debug(p)
	case packettype.GET_SERVER_INFO_RECEIVE:
		if PingCommand != nil {
			PingCommand.HandleMessageWith(read.ReadString(), a)
		}
	default:
		return false
	}
	return true
}

// Close 对应 close(groupNet)
func (a *AbstractNetConnect) Close(groupNet IGroupNet) {
	if groupNet != nil {
		if err := a.ConnectionAgreement.Close(groupNet); err != nil {
			log.Errorf("Close Connect: %v", err)
		}
		return
	}
	if err := a.ConnectionAgreement.Close(nil); err != nil {
		log.Errorf("Close Connect: %v", err)
	}
}