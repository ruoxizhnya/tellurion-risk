package pgstore

import (
	"os"
	"testing"
)

// testDSNEnv 是本仓真库测试**唯一**的 DSN 环境变量名（RISK-TEST-DSN-UNIFY）。
//
// 历史上 `RISK_TEST_DSN`（actor/migrate/monitor 三条腿）与
// `TELLURION_RISK_TEST_DSN`（approve 腿）并存，设一个名只跑一半真库腿、
// 另一半静默 skip，属于假绿。现在四条腿统一走 requireTestDSN，改名只需改这里。
const testDSNEnv = "RISK_TEST_DSN"

// requireTestDSN 取统一的测试 DSN；未设置时显式 skip（不是假绿）。
//
// DSN 只允许指向 `*_test` 库（runbook「真库腿」一节）；活库上的迁移与清理
// 不允许借测试执行。
func requireTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(testDSNEnv)
	if dsn == "" {
		t.Skip(testDSNEnv + " not set")
	}
	return dsn
}
