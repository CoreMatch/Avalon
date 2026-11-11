package systemprops

import "testing"

// TestInitParsesDashD -Dkey=value 解析 (对应 Java -D 系统属性)
//
// 全局 props 是包级单例, 测试用唯一键避免互相污染.
func TestInitParsesDashD(t *testing.T) {
	Init([]string{
		"-Dt1.key=value",
		"-Dt1.empty",   // 无 = 号: 值为空串
		"-Dt1.eq=a=b",  // 值里带 =: 只按第一个 = 切
		"not-a-prop",   // 非 -D 参数忽略
		"-D",           // 裸 -D 忽略
	})
	if v, ok := Get("t1.key"); !ok || v != "value" {
		t.Fatalf("t1.key = %q/%v", v, ok)
	}
	if v, ok := Get("t1.empty"); !ok || v != "" {
		t.Fatalf("t1.empty = %q/%v (无 = 号应为空串且存在)", v, ok)
	}
	if v, ok := Get("t1.eq"); !ok || v != "a=b" {
		t.Fatalf("t1.eq = %q/%v (只应按第一个 = 切)", v, ok)
	}
	if _, ok := Get("not-a-prop"); ok {
		t.Fatal("非 -D 参数不应入表")
	}
}

// TestSetGet 显式设置与缺失键
func TestSetGet(t *testing.T) {
	Set("t2.key", "v2")
	if v, ok := Get("t2.key"); !ok || v != "v2" {
		t.Fatalf("t2.key = %q/%v", v, ok)
	}
	if _, ok := Get("t2.missing"); ok {
		t.Fatal("缺失键应 ok=false")
	}
}

// TestGetOrEnv 属性优先, 回落环境变量
func TestGetOrEnv(t *testing.T) {
	Set("t3.key", "from-props")
	t.Setenv("t3.key", "from-env")
	t.Setenv("t3.envonly", "env-val")

	if v, ok := GetOrEnv("t3.key"); !ok || v != "from-props" {
		t.Fatalf("属性应优先于环境变量: %q/%v", v, ok)
	}
	if v, ok := GetOrEnv("t3.envonly"); !ok || v != "env-val" {
		t.Fatalf("无属性时应回落环境变量: %q/%v", v, ok)
	}
	if _, ok := GetOrEnv("t3.nothing"); ok {
		t.Fatal("两边都没有应 ok=false")
	}
}
