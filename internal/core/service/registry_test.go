package service

import (
	"errors"
	"testing"

	"rwhps/internal/net/core"
	netpacket "rwhps/internal/net/core/server/netpacket"
)

// fakeProtocol 实现 core.IRwHps
type fakeProtocol struct{ t core.NetType }

func (f *fakeProtocol) NetType() core.NetType                          { return f.t }
func (f *fakeProtocol) TypeConnect() core.TypeConnect                  { return nil }
func (f *fakeProtocol) AbstractNetPacket() netpacket.AbstractNetPacket { return nil }

// TestKey 拼接规则与原版一致 (Kind + Name)
func TestKey(t *testing.T) {
	if got := Key(KindIRwHps, "ServerProtocol"); got != "IRwHpsServerProtocol" {
		t.Errorf("Key = %q", got)
	}
}

// TestRegisterLookup 注册/查找/覆盖语义
func TestRegisterLookup(t *testing.T) {
	r := New()
	a := &fakeProtocol{t: core.HttpProtocol}
	b := &fakeProtocol{t: core.RemoteControlProtocol}

	if err := r.RegisterDefault(KindIRwHps, "HttpProtocol", a); err != nil {
		t.Fatalf("register: %v", err)
	}
	// 重复注册默认不覆盖 (对应 cover=false)
	if err := r.RegisterDefault(KindIRwHps, "HttpProtocol", b); err != nil {
		t.Fatalf("register again: %v", err)
	}
	v, ok := r.Lookup(KindIRwHps, "HttpProtocol")
	if !ok || v.(*fakeProtocol) != a {
		t.Error("first registration should win")
	}
	// cover=true 覆盖
	if err := r.Register(KindIRwHps, "HttpProtocol", b, true); err != nil {
		t.Fatalf("cover: %v", err)
	}
	v, _ = r.Lookup(KindIRwHps, "HttpProtocol")
	if v.(*fakeProtocol) != b {
		t.Error("cover should replace")
	}
}

// TestLookupMissing 未注册返回 ErrNotRegistered
func TestLookupMissing(t *testing.T) {
	r := New()
	_, err := r.MustLookup(KindProtocolType, "RelayProtocol")
	var missing *ErrNotRegistered
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want ErrNotRegistered", err)
	}
	if missing.Error() != "ProtocolType:RelayProtocol" {
		t.Errorf("message = %q", missing.Error())
	}
}

// TestTypeMismatch 类型不匹配时拒绝注册, 对应 findSuperClass 校验
func TestTypeMismatch(t *testing.T) {
	r := New()
	var wrong *ErrTypeMismatch
	if err := r.RegisterDefault(KindIRwHps, "X", "not a protocol"); !errors.As(err, &wrong) {
		t.Errorf("err = %v, want ErrTypeMismatch", err)
	}
	if err := r.RegisterDefault(KindIRwHps, "X", nil); !errors.As(err, &wrong) {
		t.Errorf("nil err = %v, want ErrTypeMismatch", err)
	}
}

// TestNames 按 Kind 前缀列出
func TestNames(t *testing.T) {
	r := New()
	_ = r.RegisterDefault(KindIRwHps, "A", &fakeProtocol{})
	_ = r.RegisterDefault(KindIRwHps, "B", &fakeProtocol{})
	_ = r.RegisterDefault(KindCore, "C", "x")
	names := r.Names(KindIRwHps)
	if len(names) != 2 {
		t.Errorf("names = %v", names)
	}
}
