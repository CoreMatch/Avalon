package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"rwhps/internal/io/packettype"
	"rwhps/internal/util/file"
	"rwhps/internal/util/file/plugindata"
)

// --- 前导 ---

// encodePreamble 按契约构造 24 字节前导 (测试侧编码, 与 Cascade
// internal/backend/transport.go EncodePreamble 同构)
func encodePreamble(t *testing.T, sessionID []byte, version, size uint16, magic string) []byte {
	t.Helper()
	h := make([]byte, PreambleSize)
	copy(h[0:4], magic)
	binary.BigEndian.PutUint16(h[4:6], version)
	binary.BigEndian.PutUint16(h[6:8], size)
	copy(h[8:], sessionID)
	return h
}

func uuidv4(t *testing.T) []byte {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return b[:]
}

func TestParsePreambleOK(t *testing.T) {
	id := uuidv4(t)
	sid, err := ParsePreamble(encodePreamble(t, id, PreambleVersion, PreambleSize, "CASC"))
	if err != nil {
		t.Fatalf("ParsePreamble: %v", err)
	}
	if len(sid) != 36 || sid[8] != '-' || sid[13] != '-' || sid[18] != '-' || sid[23] != '-' {
		t.Fatalf("会话 ID 形状 = %q", sid)
	}
	// 回环: 文本 UUID 的十六进制应与原字节一致
	compact := strings.ReplaceAll(sid, "-", "")
	for i := 0; i < 16; i++ {
		var v byte
		for j := 0; j < 2; j++ {
			c := compact[i*2+j]
			var d byte
			switch {
			case c >= '0' && c <= '9':
				d = c - '0'
			case c >= 'a' && c <= 'f':
				d = c - 'a' + 10
			default:
				t.Fatalf("非法十六进制字符 %q", c)
			}
			v = v<<4 | d
		}
		if v != id[i] {
			t.Fatalf("字节 %d = %x, 期望 %x", i, v, id[i])
		}
	}
}

func TestParsePreambleRejects(t *testing.T) {
	id := uuidv4(t)
	cases := []struct {
		name string
		h    []byte
		want error
	}{
		{"magic 错误", encodePreamble(t, id, 1, 24, "XXXX"), ErrPreambleMagic},
		{"旧 magic MCAG", encodePreamble(t, id, 1, 24, "MCAG"), ErrPreambleMagic},
		{"版本不受支持", encodePreamble(t, id, 2, 24, "CASC"), ErrPreambleVersion},
		{"头长错误", encodePreamble(t, id, 1, 25, "CASC"), ErrPreambleSize},
		{"长度不足", []byte("CASC"), ErrPreambleSize},
	}
	for _, c := range cases {
		if _, err := ParsePreamble(c.h); !errors.Is(err, c.want) {
			t.Fatalf("%s: err = %v, 期望 %v", c.name, err, c.want)
		}
	}

	// 非 UUIDv4 (版本半字节被抹掉)
	bad := uuidv4(t)
	bad[6] &= 0x0f
	if _, err := ParsePreamble(encodePreamble(t, bad, 1, 24, "CASC")); !errors.Is(err, ErrSessionIDNotV4) {
		t.Fatalf("非 UUIDv4: err = %v, 期望 ErrSessionIDNotV4", err)
	}
}

// --- JWT ---

// testSigner 与 Cascade internal/proxy/token.go Sign 同构的测试签发器
type testSigner struct {
	issuer string
	kid    string
	key    ed25519.PrivateKey
}

func (s *testSigner) sign(t *testing.T, aud, sid, sub, jti string, iat, exp int64) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "EdDSA", "kid": s.kid, "typ": "JWT"})
	claims, _ := json.Marshal(TokenClaims{Iss: s.issuer, Aud: aud, Sid: sid, Sub: sub, Iat: iat, Exp: exp, Jti: jti})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(s.key, []byte(unsigned)))
}

func newVerifierFixture(t *testing.T) (*TokenVerifier, *testSigner) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// 信任三档 (DECISIONS D-24): 校验器只做结构化声明校验, 不装载公钥;
	// 测试签发器仍真签名, 证明"不验签也接受合法形状"与"验不验签与声明无关".
	return NewTokenVerifier("backend-a"), &testSigner{issuer: "cascade", kid: "kid-1", key: priv}
}

func TestJWTVerifyOK(t *testing.T) {
	v, s := newVerifierFixture(t)
	now := time.Now()
	token := s.sign(t, "backend-a", "sid-1", "player-uuid", "jti-1", now.Unix(), now.Add(30*time.Second).Unix())
	claims, err := v.Verify(token, now)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Sub != "player-uuid" || claims.Sid != "sid-1" || claims.Jti != "jti-1" {
		t.Fatalf("claims = %+v", claims)
	}
}

// TestJWTVerifyIgnoresSignature 信任三档下不验签 (DECISIONS D-24):
// 签名被篡改的令牌只要声明合法仍被接受 —— 对端身份由连接层信任承担.
func TestJWTVerifyIgnoresSignature(t *testing.T) {
	v, s := newVerifierFixture(t)
	now := time.Now()
	token := s.sign(t, "backend-a", "sid", "sub", "jti", now.Unix(), now.Add(30*time.Second).Unix())
	parts := strings.Split(token, ".")
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sig[0] ^= 0xff
	parts[2] = base64.RawURLEncoding.EncodeToString(sig)
	if _, err := v.Verify(strings.Join(parts, "."), now); err != nil {
		t.Fatalf("不验签: 签名篡改不应影响结构化校验: %v", err)
	}
}

func TestJWTVerifyRejects(t *testing.T) {
	v, s := newVerifierFixture(t)
	now := time.Now()
	iat, exp := now.Unix(), now.Add(30*time.Second).Unix()

	cases := []struct {
		name  string
		token string
		want  error
	}{
		{"aud 不符", s.sign(t, "other-backend", "sid", "sub", "jti", iat, exp), ErrTokenClaims},
		{"kid 不同仍接受 (不校验)", func() string {
			s2 := *s
			s2.kid = "kid-2"
			return s2.sign(t, "backend-a", "sid", "sub", "jti", iat, exp)
		}(), nil},
		{"已过期", s.sign(t, "backend-a", "sid", "sub", "jti", now.Add(-60*time.Second).Unix(), now.Add(-30*time.Second).Unix()), ErrTokenExpired},
		{"寿命超 30 秒", s.sign(t, "backend-a", "sid", "sub", "jti", iat, iat+61), ErrTokenClaims},
		{"sub 为空", s.sign(t, "backend-a", "sid", "", "jti", iat, exp), ErrTokenClaims},
		{"结构残缺", "abc.def", ErrTokenMalformed},
	}
	for _, c := range cases {
		_, err := v.Verify(c.token, now)
		if c.want == nil {
			if err != nil {
				t.Fatalf("%s: 不应拒绝: %v", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: err = %v, 期望 %v", c.name, err, c.want)
		}
	}
}

// --- jti 账本 ---

// jtiFile 在临时目录里开一个独立账本文件 (不走 GetFolder: 那是相对数据根的)
func jtiFile(dir string) *file.FileUtils {
	return &file.FileUtils{Path: dir + "/jti.bin"}
}

func newJtiStore(t *testing.T) *JtiStore {
	t.Helper()
	store, err := plugindata.Open(jtiFile(t.TempDir()))
	if err != nil {
		t.Fatalf("plugindata.Open: %v", err)
	}
	return NewJtiStore(store)
}

func TestJtiStoreConsumeOnce(t *testing.T) {
	s := newJtiStore(t)
	exp := time.Now().Add(30 * time.Second).Unix()
	if err := s.Consume("jti-1", exp); err != nil {
		t.Fatalf("首次消费: %v", err)
	}
	if err := s.Consume("jti-1", exp); !errors.Is(err, ErrJtiReplayed) {
		t.Fatalf("重复消费: %v, 期望 ErrJtiReplayed", err)
	}
	if s.Size() != 1 {
		t.Fatalf("Size = %d", s.Size())
	}
}

func TestJtiStorePersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	exp := time.Now().Add(30 * time.Second).Unix()

	store1, err := plugindata.Open(jtiFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	s1 := NewJtiStore(store1)
	if err := s1.Consume("jti-x", exp); err != nil {
		t.Fatal(err)
	}

	// 模拟重启: 重新打开同一文件
	store2, err := plugindata.Open(jtiFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	s2 := NewJtiStore(store2)
	if err := s2.Consume("jti-x", exp); !errors.Is(err, ErrJtiReplayed) {
		t.Fatalf("重启后重复消费: %v, 期望 ErrJtiReplayed", err)
	}
}

func TestJtiStorePrunesExpired(t *testing.T) {
	dir := t.TempDir()
	store, err := plugindata.Open(jtiFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	s := NewJtiStore(store)
	// 一条早已过期的记录 (超过宽限期)
	s.Consume("jti-old", time.Now().Unix()-int64(pruneGrace)-100)
	// 重新打开时应被清理
	store2, err := plugindata.Open(jtiFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	s2 := NewJtiStore(store2)
	if s2.Size() != 0 {
		t.Fatalf("过期条目未清理, Size = %d", s2.Size())
	}
}

// --- 会话状态机 ---

func TestSessionGateAllowsOnlyPreregisterWhenPending(t *testing.T) {
	r := NewRegistry(time.Second)
	closed := false
	s := r.Register("sid-1", func() { closed = true })

	if !s.AllowPacket(int(packettype.PREREGISTER_INFO_RECEIVE)) {
		t.Fatal("PENDING 应放行 Preregister(160)")
	}
	if s.AllowPacket(int(packettype.REGISTER_PLAYER)) {
		t.Fatal("PENDING 不得放行 RegisterPlayer(110)")
	}
	if s.AllowPacket(int(packettype.CHAT_RECEIVE)) {
		t.Fatal("PENDING 不得放行聊天包")
	}

	if err := r.Bind("sid-1", "player-1", "jti-1"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if s.State() != StateBound {
		t.Fatalf("state = %v", s.State())
	}
	if s.PlayerID() != "player-1" {
		t.Fatalf("PlayerID = %q", s.PlayerID())
	}
	if !s.AllowPacket(int(packettype.REGISTER_PLAYER)) {
		t.Fatal("BOUND 应放行 RegisterPlayer")
	}
	_ = closed
}

func TestRegistryBindIdempotentAndConflict(t *testing.T) {
	r := NewRegistry(time.Second)
	r.Register("sid-1", nil)

	if err := r.Bind("sid-1", "player-1", "jti-1"); err != nil {
		t.Fatal(err)
	}
	// 完全相同的重试: 幂等接受
	if err := r.Bind("sid-1", "player-1", "jti-1"); err != nil {
		t.Fatalf("幂等重试: %v", err)
	}
	// 换 jti / 换身份: 冲突
	if err := r.Bind("sid-1", "player-1", "jti-2"); !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("换 jti: %v, 期望冲突", err)
	}
	if err := r.Bind("sid-1", "player-2", "jti-1"); !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("换身份: %v, 期望冲突", err)
	}
	// 不存在的会话
	if err := r.Bind("nope", "p", "j"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("未知会话: %v, 期望 ErrSessionNotFound", err)
	}
}

func TestRegistryPendingTimeoutCloses(t *testing.T) {
	r := NewRegistry(30 * time.Millisecond)
	closed := make(chan struct{})
	r.Register("sid-t", func() { close(closed) })

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("pending 超时应关闭连接")
	}
	if r.Get("sid-t") != nil {
		t.Fatal("超时时会话应被移除")
	}
	// 超时后绑定应失败
	if err := r.Bind("sid-t", "p", "j"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("超时后绑定: %v, 期望 ErrSessionNotFound", err)
	}
}

func TestRegistryRemoveOnDisconnect(t *testing.T) {
	r := NewRegistry(time.Hour)
	r.Register("sid-d", func() { t.Fatal("正常结束不应触发关闭回调") })
	r.Remove("sid-d")
	if r.Get("sid-d") != nil {
		t.Fatal("Remove 后会话应消失")
	}
	if err := r.Bind("sid-d", "p", "j"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Remove 后绑定: %v", err)
	}
}
