package net

import (
	"bytes"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"rwhps/internal/io/packet"
	"rwhps/internal/io/packettype"
	"rwhps/internal/net/core"
	netpacket "rwhps/internal/net/core/server/netpacket"
)

// TestWireRoundTrip 验证 TCP 帧编解码互通
func TestWireRoundTrip(t *testing.T) {
	inner := core.NewNullRwHps(core.ServerProtocol)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	// 统计 hook 被触发次数, 验证 NetConnectNewEvent 每次新连接只触发一次
	var hookMu sync.Mutex
	hookCount := 0
	core.NetConnectNewEventHook = func(_ *core.ConnectionAgreement) bool {
		hookMu.Lock()
		hookCount++
		hookMu.Unlock()
		return false
	}
	defer func() { core.NetConnectNewEventHook = nil }()

	// 注入一个测试用 hook 在 server 收到第一个 packet 时存储
	var gotMu sync.Mutex
	var gotType int
	var gotBytes []byte
	gotCh := make(chan struct{}, 1)

	// 替换 TypeConnect, 让其捕获 server 端首个 packet
	rwHps := &capturingServerRwHps{
		rwHps: inner,
		onPacket: func(p *packet.Packet) {
			gotMu.Lock()
			gotType = p.Type.TypeInt()
			gotBytes = append([]byte{}, p.Bytes...)
			gotMu.Unlock()
			select {
			case gotCh <- struct{}{}:
			default:
			}
		},
	}
	an := NewAbstractNet(rwHps)

	var serverWg sync.WaitGroup
	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		serverConn, err := listener.Accept()
		if err != nil {
			return
		}
		an.HandleChannel(core.NewChannel(serverConn))
	}()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	pkt := packet.NewPacket(packettype.PREREGISTER_INFO_RECEIVE, []byte{0xCA, 0xFE, 0xBA, 0xBE})
	frame := GamePacketEncoder(pkt)
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case <-gotCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for packet")
	}
	// 关闭 client 连接, 让 server 侧 read 循环退出
	_ = conn.Close()
	serverWg.Wait()

	gotMu.Lock()
	if gotType != int(packettype.PREREGISTER_INFO_RECEIVE) {
		t.Errorf("server got type %d, want %d", gotType, packettype.PREREGISTER_INFO_RECEIVE)
	}
	if !bytes.Equal(gotBytes, []byte{0xCA, 0xFE, 0xBA, 0xBE}) {
		t.Errorf("server got bytes %x, want cafebabe", gotBytes)
	}
	gotMu.Unlock()

	hookMu.Lock()
	defer hookMu.Unlock()
	if hookCount != 1 {
		t.Errorf("NetConnectNewEventHook triggered %d times, want 1", hookCount)
	}
}

// capturingServerRwHps 注入的测试用 IRwHps, 捕获 server 端首个 packet
type capturingServerRwHps struct {
	rwHps    core.IRwHps
	onPacket func(*packet.Packet)
}

func (f *capturingServerRwHps) NetType() core.NetType { return f.rwHps.NetType() }
func (f *capturingServerRwHps) TypeConnect() core.TypeConnect {
	return &capturingTypeConnect{onPacket: f.onPacket}
}
func (f *capturingServerRwHps) AbstractNetPacket() netpacket.AbstractNetPacket {
	return nil
}

type capturingTypeConnect struct {
	anc      *core.AbstractNetConnect
	onPacket func(*packet.Packet)
}

func (n *capturingTypeConnect) GetTypeConnect(ca *core.ConnectionAgreement) core.TypeConnect {
	n.anc = core.NewAbstractNetConnect(ca)
	return n
}
func (n *capturingTypeConnect) SetData(_ any)                                       {}
func (n *capturingTypeConnect) ProcessConnect(p *packet.Packet) error {
	if n.onPacket != nil && p != nil {
		n.onPacket(p)
	}
	return nil
}
func (n *capturingTypeConnect) AbstractNetConnect() *core.AbstractNetConnect { return n.anc }
func (n *capturingTypeConnect) Version() string                              { return "wiretest" }