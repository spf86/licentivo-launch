package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"licentivo-launch/internal/adapters/licentivo"
)

func TestLicentivoConnectionFailureExplainsStageAndAction(t *testing.T) {
	for _, test := range []struct {
		status                        int
		remoteCode, wantCode, message string
	}{
		{400, "LAUNCH_HTTPS_REQUIRED", "LAUNCH_HTTPS_REQUIRED", "接入设置"},
		{403, "LAUNCH_HTTPS_REQUIRED", "LAUNCH_HTTPS_REQUIRED", "接入设置"},
		{401, "UNAUTHENTICATED", "LICENSE_KEY_INVALID", "已到期或已吊销"},
		{403, "FORBIDDEN", "LICENSE_KEY_FORBIDDEN", "读写 Key"},
		{409, "LAUNCH_TRUST_IMMUTABLE", "LAUNCH_TRUST_IMMUTABLE", "重新建立连接"},
		{403, "LAUNCH_TRUST_IMMUTABLE", "LAUNCH_TRUST_IMMUTABLE", "重新建立连接"},
		{400, "INVALID_INPUT", "LICENSE_CONNECTION_FAILED", "issuer is invalid"},
	} {
		err := licentivoConnectionError("建立 sandbox 账号连接", &licentivo.APIError{Status: test.status, Code: test.remoteCode, Message: "issuer is invalid", RequestID: "request-http-test"})
		var detail *Error
		if !errors.As(err, &detail) || detail.Code != test.wantCode || !strings.Contains(detail.Message, test.message) || !strings.Contains(detail.Message, "建立 sandbox 账号连接失败") || !strings.Contains(detail.Message, "request-http-test") {
			t.Fatal("missing actionable connection diagnostics", err)
		}
	}
	err := licentivoConnectionError("读取产品", &licentivo.TransportError{Reason: "LICENTIVO_TIMEOUT", Cause: context.DeadlineExceeded}).(*Error)
	if err.Code != "LICENTIVO_TIMEOUT" || !strings.Contains(err.Message, "超时") {
		t.Fatal("timeout was hidden", err)
	}
	conflict := licentivoConnectionError("建立账号连接", licentivo.ErrConflict).(*Error)
	if conflict.Code != "LICENSE_CONNECTION_CONFLICT" || !strings.Contains(conflict.Message, "环境不一致") {
		t.Fatal("a rejected connection response was incorrectly reported as a network failure")
	}
}
