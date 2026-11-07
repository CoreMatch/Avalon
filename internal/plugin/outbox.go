package plugin

import (
	"strings"
	"sync"

	"github.com/CoreMatch/Avalon/internal/util/file"
	"github.com/CoreMatch/Avalon/internal/util/jsonutil"
	"github.com/CoreMatch/Avalon/internal/util/log"
)

// outboxRecord 是 WAL 里的一行: 事件正文在入队时即序列化固定,
// 重试与重放都复用同一份字节 (签名与 eventId 因此稳定).
type outboxRecord struct {
	EventID      string `json:"eventId"`
	Event        string `json:"event"`
	EnqueuedAtMs int64  `json:"enqueuedAtMs"`
	Body         string `json:"body"`
}

// Outbox 是单个插件的持久化投递队列 (JSONL 文件即写前日志).
//
// 追加发生在入队时刻, 移除只发生在收到签名认可的 2xx 之后; 进程在两者之间
// 崩溃时, 重启重放会再次投递该事件 —— 由 eventId 的插件侧去重兜底
// (契约 "Delivery guarantees").
type Outbox struct {
	mu   sync.Mutex
	file *file.FileUtils
	cap  int
	recs []outboxRecord
}

// openOutbox 载入既有 WAL; 损坏的行跳过并告警 (不得让一个坏行挡住重放)
func openOutbox(f *file.FileUtils, capacity int) *Outbox {
	o := &Outbox{file: f, cap: capacity}
	data := f.ReadFileStringData()
	for i, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec outboxRecord
		if err := jsonUnmarshal([]byte(line), &rec); err != nil || rec.EventID == "" || rec.Body == "" {
			log.Warnf("[Plugin]", "outbox {0}: dropping corrupt line {1}", f.Name(), i+1)
			continue
		}
		o.recs = append(o.recs, rec)
	}
	return o
}

// append 入队; 容量满时丢最旧 (保最新), 返回被丢弃的条数
func (o *Outbox) append(rec outboxRecord) (dropped int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for len(o.recs) >= o.cap {
		o.recs = o.recs[1:]
		dropped++
	}
	o.recs = append(o.recs, rec)
	o.persist()
	return dropped
}

// peek 取队首 (不移除)
func (o *Outbox) peek() (outboxRecord, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.recs) == 0 {
		return outboxRecord{}, false
	}
	return o.recs[0], true
}

// pop 移除队首并重写 WAL
func (o *Outbox) pop() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.recs) == 0 {
		return
	}
	o.recs = o.recs[1:]
	o.persist()
}

// filterDrop 删除满足 drop 条件的记录 (返回条数), 用于协商后清理
// 版本不匹配的积压事件
func (o *Outbox) filterDrop(drop func(outboxRecord) bool) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	kept := o.recs[:0]
	n := 0
	for _, r := range o.recs {
		if drop(r) {
			n++
			continue
		}
		kept = append(kept, r)
	}
	o.recs = kept
	if n > 0 {
		o.persist()
	}
	return n
}

// size 当前积压条数
func (o *Outbox) size() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.recs)
}

// persist 全量重写 WAL (事件量级小, 全量重写最简单且无碎片)
//
// 调用方必须持有 o.mu. 写失败只记日志: 内存队列仍然有效, 下次变更再写.
func (o *Outbox) persist() {
	var b strings.Builder
	for _, r := range o.recs {
		b.WriteString(jsonutil.ToJSON(r))
		b.WriteByte('\n')
	}
	o.file.WriteFileBytes([]byte(b.String()))
}
