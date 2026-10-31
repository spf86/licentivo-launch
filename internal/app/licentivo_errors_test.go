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

func TestPlanMappingFailurePreservesRemoteCause(t *testing.T) {
	for _, test := range []struct {
		remote *licentivo.APIError
		status int
		code   string
	}{
		{&licentivo.APIError{Status: 409, Code: "LAUNCH_POLICY_UNSUPPORTED", RequestID: "mapping-unsupported"}, 409, "LAUNCH_POLICY_UNSUPPORTED"},
		{&licentivo.APIError{Status: 409, Code: "POLICY_VERSION_CONFLICT", RequestID: "mapping-version"}, 409, "LICENSE_POLICY_CHANGED"},
		{&licentivo.APIError{Status: 409, Code: "PLAN_PRODUCT_IMMUTABLE", RequestID: "mapping-product"}, 409, "PLAN_PRODUCT_IMMUTABLE"},
		{&licentivo.APIError{Status: 503, Code: "SERVICE_UNAVAILABLE", RequestID: "mapping-outage"}, 503, "LICENSE_CONNECTION_FAILED"},
	} {
		got := licentivoPlanMappingError(test.remote).(*Error)
		if got.Status != test.status || got.Code != test.code || !strings.Contains(got.Message, test.remote.RequestID) || !strings.Contains(got.Message, test.remote.Code) {
			t.Fatalf("lost mapping cause: %+v", got)
		}
	}
	got := licentivoPlanMappingError(&licentivo.TransportError{Reason: "LICENTIVO_TIMEOUT", Cause: context.DeadlineExceeded}).(*Error)
	if got.Status != 503 || got.Code != "LICENTIVO_TIMEOUT" {
		t.Fatalf("timeout mislabeled as changed policy: %+v", got)
	}
}

func TestDeviceFailurePreservesDeterministicRejection(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{
		{409, "IDEMPOTENCY_CONFLICT"}, {403, "SELF_RELEASE_LIMIT"},
		{404, "NOT_FOUND"}, {400, "INVALID_INPUT"},
	} {
		got := licentivoDeviceError(&licentivo.APIError{Status: tc.status, Code: tc.code, RequestID: "remote-device-1"}).(*Error)
		if got.Status != tc.status || got.Code != tc.code || !strings.Contains(got.Message, "remote-device-1") {
			t.Fatalf("deterministic device rejection lost: %+v", got)
		}
	}
	for _, remote := range []*licentivo.APIError{
		{Status: 401, Code: "UNAUTHENTICATED"}, {Status: 429, Code: "RATE_LIMITED"},
		{Status: 503, Code: "SERVICE_UNAVAILABLE"}, {Status: 200, Code: "LICENTIVO_RESPONSE_INVALID"},
	} {
		got := licentivoDeviceError(remote).(*Error)
		if got.Status != 503 || got.Code != "LICENTIVO_BRIDGE_UNAVAILABLE" {
			t.Fatalf("bridge failure was attributed to the customer's session: %+v", got)
		}
	}
	if got := licentivoDeviceError(context.DeadlineExceeded).(*Error); got.Status != 503 {
		t.Fatal("transport error should remain temporary")
	}
}
