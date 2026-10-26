package main

import "testing"

// TestDefaultAddr 钉住 ADR-036 G0 的默认监听端口。
//
// 8086 是 ai-service 的口径，本仓回到 8086 就会和同机联调撞号；这条测试是
// `deploy/compose.yaml`（8089）与代码默认值之间唯一的机械对拍点。
func TestDefaultAddr(t *testing.T) {
	if defaultAddr != "127.0.0.1:8089" {
		t.Fatalf("default listen address = %q, want 127.0.0.1:8089 (ADR-036 G0)", defaultAddr)
	}
}
