package plugin

import (
	"strings"
	"sync"

	"github.com/CoreMatch/Avalon/internal/util/file"
	"github.com/CoreMatch/Avalon/internal/util/jsonutil"
	"github.com/CoreMatch/Avalon/internal/util/log"
)

// outboxRecord 是队列里的一条事件.
//
// Persist=true (deliver 档, 如 game.over): 入队即写 WAL, 应答确认才移除,
// 重启重放, 同一 eventId 由插件去重兜底 (契约 "Delivery guarantees").
// Persist=false (observe 档): 只存在于运行期内存队列, 不落盘、不重试、
// 不重放 —— 观察类事件过期即无价值.
//
// 追加发生在入队时刻, 移除只发生在收到签名认可的 2xx 之后; 进程在两者之间
// 崩溃时, 重启重放会再次投递该事件 —— 由 eventId 的插件侧去重兜底.
type outboxRecord struct {
	EventID      string `json:"eventId"`
	Event        string `json:"event"`
	EnqueuedAtMs int64  `json:"enqueuedAtMs"`
	Body         string `json:"body"`

	// Persist 是否落 WAL (运行期字段, 不序列化: WAL 里只有持久记录)
	Persist bool `json:"-"`
}

// Outbox 是单个插件的投递队列: 持久记录 (deliver 档) 以 JSONL 文件作写前
// 日志, 观察记录 (observe 档) 仅驻内存. 两档共用一条 FIFO, 保持该插件的
// 投递顺序 = 入队顺序.
type Outbox struct {
	mu   sync.Mutex
	file *file.FileUtils
	cap  int
	recs []outboxRecord
}

// openOutbox 载入既有 WAL; 损坏的行跳过并告警 (不得让一个坏行挡住重放).
// 载入的记录一律视为持久记录.
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
		rec.Persist = true
		o.recs = append(o.recs, rec)
	}
	return o
}

// append 入队; 容量满时丢最旧 (保最新), 返回被丢弃的条数.
// 持久记录入队即落盘.
func (o *Outbox) append(rec outboxRecord) (dropped int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for len(o.recs) >= o.cap {
		o.recs = o.recs[1:]
		dropped++
	}
	o.recs = append(o.recs, rec)
	if rec.Persist {
		o.persist()
	}
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

// pop 移除队首; 移除的是持久记录时重写 WAL
func (o *Outbox) pop() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.recs) == 0 {
		return
	}
	head := o.recs[0]
	o.recs = o.recs[1:]
	if head.Persist {
		o.persist()
	}
}

// filterDrop 删除满足 drop 条件的记录 (返回条数), 用于协商后清理
// 版本不匹配的积压事件
func (o *Outbox) filterDrop(drop func(outboxRecord) bool) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	kept := o.recs[:0]
	n := 0
	persistedDropped := false
	for _, r := range o.recs {
		if drop(r) {
			n++
			if r.Persist {
				persistedDropped = true
			}
			continue
		}
		kept = append(kept, r)
	}
	o.recs = kept
	if persistedDropped {
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

// persist 全量重写 WAL (事件量级小, 全量重写最简单且无碎片).
// 只写持久记录: 观察档记录只存在于内存队列, 不落盘也不重放.
//
// 调用方必须持有 o.mu. 写失败只记日志: 内存队列仍然有效, 下次变更再写.
func (o *Outbox) persist() {
	var b strings.Builder
	for _, r := range o.recs {
		if !r.Persist {
			continue
		}
		b.WriteString(jsonutil.ToJSON(r))
		b.WriteByte('\n')
	}
	o.file.WriteFileBytes([]byte(b.String()))
}
