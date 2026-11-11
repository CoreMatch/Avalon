package console

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestReadLine 提示符输出与整行读取 (去行尾)
func TestReadLine(t *testing.T) {
	out := &bytes.Buffer{}
	c := New(strings.NewReader("hello\nworld\n"), out)

	line, err := c.ReadLine("> ")
	if err != nil || line != "hello" {
		t.Fatalf("ReadLine = %q/%v", line, err)
	}
	if out.String() != "> " {
		t.Fatalf("提示符应输出一次, got %q", out.String())
	}

	line, err = c.ReadLine("")
	if err != nil || line != "world" {
		t.Fatalf("ReadLine = %q/%v", line, err)
	}

	// 输入耗尽 -> ErrClosed
	if _, err = c.ReadLine(""); !errors.Is(err, ErrClosed) {
		t.Fatalf("耗尽后应为 ErrClosed, got %v", err)
	}
}

// TestReadLineCRLF 与无尾换行的最后一行
func TestReadLineCRLF(t *testing.T) {
	c := New(strings.NewReader("a\r\nb"), nil)
	if line, err := c.ReadLine(""); err != nil || line != "a" {
		t.Fatalf("CRLF = %q/%v", line, err)
	}
	// EOF 前的最后一段无换行也算有效输入
	if line, err := c.ReadLine(""); err != nil || line != "b" {
		t.Fatalf("无尾换行 = %q/%v", line, err)
	}
}

// TestNewNilInput 无输入源立即 ErrClosed
func TestNewNilInput(t *testing.T) {
	c := New(nil, nil)
	if _, err := c.ReadLine(""); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil 输入应为 ErrClosed, got %v", err)
	}
}

// TestNewReusesBufio 传入 *bufio.Reader 直接复用 (不再套缓冲, 见 New 注释)
func TestNewReusesBufio(t *testing.T) {
	br := bufio.NewReader(strings.NewReader("x\n"))
	c := New(br, nil)
	if line, _ := c.ReadLine(""); line != "x" {
		t.Fatalf("ReadLine = %q", line)
	}
}

// TestClose 关闭后一律 ErrClosed
func TestClose(t *testing.T) {
	c := New(strings.NewReader("x\n"), nil)
	c.Close()
	if _, err := c.ReadLine(""); !errors.Is(err, ErrClosed) {
		t.Fatalf("Close 后应为 ErrClosed, got %v", err)
	}
}

// TestPrintAbovePrint 输出语义: printAbove 换行, print 不换行; nil 输出不 panic
func TestPrintAbovePrint(t *testing.T) {
	out := &bytes.Buffer{}
	c := New(nil, out)
	c.PrintAbove("line1")
	c.Print("part")
	if out.String() != "line1\npart" {
		t.Fatalf("输出 = %q", out.String())
	}

	// nil out 不 panic
	c2 := New(nil, nil)
	c2.PrintAbove("x")
	c2.Print("y")
}
