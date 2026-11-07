package plugin

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CoreMatch/Avalon/internal/util/file"
)

// --- 签名 ---

func TestSignVerifyRoundTrip(t *testing.T) {
	secret := []byte("test-secret-bytes-1234567890ab")
	body := []byte(`{"event":"game.over"}`)

	req, _ := http.NewRequest(http.MethodPost, "http://example.invalid", nil)
	signRequest(req, secret, body)

	ts := req.Header.Get(headerTimestamp)
	if ts == "" {
		t.Fatal("缺时间戳头")
	}
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set(headerTimestamp, ts)
	resp.Header.Set(headerSignature, req.Header.Get(headerSignature))
	if err := verifyResponse([][]byte{secret}, resp, body); err != nil {
		t.Fatalf("验签应通过: %v", err)
	}
}

func TestVerifyRejectsWrongSecretOrBodyOrSkew(t *testing.T) {
	secret := []byte("test-secret-bytes-1234567890ab")
	other := []byte("another-secret-1234567890abcde")
	body := []byte(`{"x":1}`)

	mkResp := func(ts int64, sig string) *http.Response {
		r := &http.Response{Header: http.Header{}}
		r.Header.Set(headerTimestamp, strconv.FormatInt(ts, 10))
		r.Header.Set(headerSignature, sig)
		return r
	}
	now := time.Now().Unix()

	// 错误密钥
	if err := verifyResponse([][]byte{other}, mkResp(now, signBody(secret, now, body)), body); err == nil {
		t.Error("错误密钥应验签失败")
	}
	// 正文被改
	if err := verifyResponse([][]byte{secret}, mkResp(now, signBody(secret, now, body)), []byte(`{"x":2}`)); err == nil {
		t.Error("正文篡改应验签失败")
	}
	// 时间戳过期
	old := now - maxClockSkew - 10
	if err := verifyResponse([][]byte{secret}, mkResp(old, signBody(secret, old, body)), body); err == nil {
		t.Error("超偏移时间戳应验签失败")
	}
	// 缺头
	bare := &http.Response{Header: http.Header{}}
	if err := verifyResponse([][]byte{secret}, bare, body); err == nil {
		t.Error("缺签名头应验签失败")
	}
}

func TestVerifyAcceptsAnyRotationKey(t *testing.T) {
	old := []byte("old-secret-bytes-1234567890abcde")
	newer := []byte("new-secret-bytes-1234567890abcde")
	body := []byte(`{}`)
	now := time.Now().Unix()

	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set(headerTimestamp, strconv.FormatInt(now, 10))
	resp.Header.Set(headerSignature, signBody(old, now, body))
	if err := verifyResponse([][]byte{newer, old}, resp, body); err != nil {
		t.Fatalf("轮换列表内任一密钥应验签通过: %v", err)
	}
}

// --- 事件 ID ---

func TestEventIDShapeAndUniqueness(t *testing.T) {
	seen := make(map[string]bool, 2000)
	now := time.Now()
	for i := 0; i < 2000; i++ {
		id := newEventID(now)
		if !eventIDPattern.MatchString(id) {
			t.Fatalf("eventId 形状不符: %q", id)
		}
		if seen[id] {
			t.Fatalf("eventId 冲突: %q", id)
		}
		seen[id] = true
	}
}

func TestEventIDTimeOrdered(t *testing.T) {
	a := newEventID(time.Now())
	b := newEventID(time.Now().Add(2 * time.Millisecond))
	if a[:14] > b[:14] {
		t.Fatalf("时间前缀应单调: %q vs %q", a, b)
	}
}

// --- 理由净化 ---

func TestSanitizeReason(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"  正常理由 ", "正常理由"},
		{"a\x00b\nc", "abc"},
		{strings.Repeat("x", 250), strings.Repeat("x", 200)},
	}
	for _, c := range cases {
		if got := sanitizeReason(c.in); got != c.want {
			t.Errorf("sanitizeReason(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- Outbox WAL ---

func TestOutboxAppendPeekPopPersists(t *testing.T) {
	dir := t.TempDir()
	f := &file.FileUtils{Path: dir + "/ob.jsonl"}

	o := openOutbox(f, 10)
	for i := 0; i < 3; i++ {
		if d := o.append(outboxRecord{EventID: "e" + string(rune('a'+i)), Event: EventGameOver, Body: "{}", Persist: true}); d != 0 {
			t.Fatalf("未满不应丢弃: %d", d)
		}
	}
	if rec, ok := o.peek(); !ok || rec.EventID != "ea" {
		t.Fatalf("peek = %+v", rec)
	}
	o.pop()
	if rec, ok := o.peek(); !ok || rec.EventID != "eb" {
		t.Fatalf("pop 后 peek = %+v", rec)
	}

	// 重新打开: 从 WAL 恢复, 且损坏行被跳过
	f.WriteFileBytes([]byte(f.ReadFileStringData() + "{corrupt\n"))
	o2 := openOutbox(f, 10)
	if got := o2.size(); got != 2 {
		t.Fatalf("重放后条数 = %d, 期望 2 (损坏行跳过)", got)
	}
}

func TestOutboxCapacityDropsOldest(t *testing.T) {
	dir := t.TempDir()
	f := &file.FileUtils{Path: dir + "/ob.jsonl"}
	o := openOutbox(f, 2)

	var totalDropped int
	for i := 0; i < 5; i++ {
		totalDropped += o.append(outboxRecord{EventID: "e" + string(rune('a'+i)), Event: EventGameOver, Body: "{}", Persist: true})
	}
	if totalDropped != 3 {
		t.Fatalf("累计丢弃 = %d, 期望 3", totalDropped)
	}
	if got := o.size(); got != 2 {
		t.Fatalf("size = %d", got)
	}
	if rec, _ := o.peek(); rec.EventID != "ed" {
		t.Fatalf("应保留最新两条, 队首 = %q", rec.EventID)
	}
}

func TestOutboxFilterDrop(t *testing.T) {
	dir := t.TempDir()
	f := &file.FileUtils{Path: dir + "/ob.jsonl"}
	o := openOutbox(f, 10)
	o.append(outboxRecord{EventID: "1", Event: EventGameOver, Body: "{}", Persist: true})
	o.append(outboxRecord{EventID: "2", Event: EventRoomCreate, Body: "{}", Persist: true})
	o.append(outboxRecord{EventID: "3", Event: EventGameOver, Body: "{}", Persist: true})

	n := o.filterDrop(func(r outboxRecord) bool { return r.Event != EventGameOver })
	if n != 1 || o.size() != 2 {
		t.Fatalf("filterDrop = %d, size = %d", n, o.size())
	}
	// 持久化: 重开仍在
	o2 := openOutbox(f, 10)
	if o2.size() != 2 {
		t.Fatalf("重开 size = %d", o2.size())
	}
}

// TestOutboxObserveRecordsNotPersisted observe 档记录不落 WAL: 重启不重放
func TestOutboxObserveRecordsNotPersisted(t *testing.T) {
	dir := t.TempDir()
	f := &file.FileUtils{Path: dir + "/ob.jsonl"}
	o := openOutbox(f, 10)
	o.append(outboxRecord{EventID: "o1", Event: EventPlayerChat, Body: "{}"}) // Persist=false
	o.append(outboxRecord{EventID: "d1", Event: EventGameOver, Body: "{}", Persist: true})

	if got := o.size(); got != 2 {
		t.Fatalf("size = %d", got)
	}
	// 重开: 只剩持久记录
	o2 := openOutbox(f, 10)
	if got := o2.size(); got != 1 {
		t.Fatalf("重开 size = %d, 期望 1 (observe 记录不重放)", got)
	}
	if rec, ok := o2.peek(); !ok || rec.EventID != "d1" {
		t.Fatalf("重开队首 = %+v", rec)
	}
	// pop 观察记录不触发落盘 (文件内容不变): 先 pop 掉 d1 之外的... 直接验证
	// 移除持久记录后文件同步收缩
	o2.pop()
	o3 := openOutbox(f, 10)
	if got := o3.size(); got != 0 {
		t.Fatalf("移除持久记录后重开 size = %d", got)
	}
}

// --- 配置 ---

func TestOutboxFileNameSanitized(t *testing.T) {
	got := outboxFileName("a/b c:中文")
	if got != "a_b_c___" && !isFileNameSafe(got) {
		t.Fatalf("净化结果不安全: %q", got)
	}
	if outboxFileName("///") == "" {
		t.Fatal("全非法字符也应有非空文件名")
	}
}

func isFileNameSafe(s string) bool {
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			return false
		}
	}
	return len(s) > 0
}
