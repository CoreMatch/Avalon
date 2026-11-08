package core

import (
	"net/url"
	"sync"
	"sync/atomic"

	gameio "github.com/CoreMatch/Avalon/internal/io"
	"github.com/CoreMatch/Avalon/internal/io/packet"
	"github.com/CoreMatch/Avalon/internal/io/packettype"
	"github.com/CoreMatch/Avalon/internal/net/temp"
	"github.com/CoreMatch/Avalon/internal/util/game/command"
	"github.com/CoreMatch/Avalon/internal/util/log"
	"github.com/CoreMatch/Avalon/internal/util/timeutil"
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

	// disconnectImpl 具体连接注入的断开实现 (见 SetDisconnect)
	disconnectImpl func()
	// disconnectOnce 保证具体断开逻辑在并发触发下只跑一遍
	// (读循环退出清理与超时扫描可能同时到达)
	disconnectOnce sync.Once
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

// ProtocolProvider 对应 protocolProvider
func (a *AbstractNetConnect) ProtocolProvider() IProtocolProvider {
	return a.ConnectionAgreement.ProtocolProvider
}

// IP 获取连接 IP
func (a *AbstractNetConnect) IP() string { return a.ConnectionAgreement.IP }

// IPLong24 获取连接 IP 长整型
func (a *AbstractNetConnect) IPLong24() string { return a.ConnectionAgreement.IPLong24 }

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

// Disconnect 断开连接: 统一入口.
//
// Go 没有虚方法, 而清理路径 (读循环 EOF/RST 退出、超时扫描、发送失败、
// 关服) 只持有基类指针; 真正的断开逻辑 (房间列表清理 / PlayerLeaveEvent /
// 引擎注销) 在子类方法里. 子类构造时注入自己的实现 (SetDisconnect),
// 这里统一分发; 未注入时为空操作 (测试脚手架).
//
// 修复实测确认的缺陷: 此前清理路径经基类指针调到的是这个空实现,
// RST/EOF 异常断连的玩家永久留在房间里 (干净断连走子类直调路径才正常).
func (a *AbstractNetConnect) Disconnect() {
	if a.disconnectImpl == nil {
		return
	}
	a.disconnectOnce.Do(a.disconnectImpl)
}

// SetDisconnect 注入具体连接的断开实现, 由子类构造器调用
func (a *AbstractNetConnect) SetDisconnect(fn func()) { a.disconnectImpl = fn }

// SendPacket 发送数据包
func (a *AbstractNetConnect) SendPacket(p *packet.Packet) {
	ca := a.ConnectionAgreement
	if err := ca.Send(p); err != nil {
		a.Disconnect()
		if ca.UseAgreementStr == "UDP" {
			log.Errorf("["+ca.UseAgreementStr+"]", "SendError - 本消息单独出现无妨 连续多次出现请debug")
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
			log.Errorf("Connect", "Close Connect: {0}", err)
		}
		return
	}
	if err := a.ConnectionAgreement.Close(nil); err != nil {
		log.Errorf("Connect", "Close Connect: {0}", err)
	}
}
