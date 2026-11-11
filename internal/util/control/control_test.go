package control

import "testing"

// TestEnumValues 枚举值钉死: 这些整数语义对应上游 Kotlin enum 的 ordinal,
// 协议与状态机代码依赖零值 = 默认放行/继续
func TestEnumValues(t *testing.T) {
	if CONTINUE != 0 || STOPPED != 1 {
		t.Fatalf("EventNext = %d/%d, want 0/1", CONTINUE, STOPPED)
	}
	if BREAK != 0 || CONTINUE_LOOP != 1 {
		t.Fatalf("ControlFind = %d/%d, want 0/1", BREAK, CONTINUE_LOOP)
	}
	if ASYNC != 0 || SYNC != 1 {
		t.Fatalf("ControlSync = %d/%d, want 0/1", ASYNC, SYNC)
	}
	if LISTENING != 0 || STOPPED_LISTEN != 1 {
		t.Fatalf("ControlListening = %d/%d, want 0/1", LISTENING, STOPPED_LISTEN)
	}
}
