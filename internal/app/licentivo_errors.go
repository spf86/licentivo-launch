package app

import (
	"context"
	"errors"
	"fmt"

	"licentivo-launch/internal/adapters/licentivo"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

func licentivoConnectionError(stage string, err error) error {
	code, message := "LICENSE_CONNECTION_FAILED", "无法连接 Licentivo，请检查测试服务器的网络、DNS 和 HTTPS 访问"
	var remote *licentivo.APIError
	var transport *licentivo.TransportError
	if errors.As(err, &remote) {
		switch {
		case remote.Code == "LAUNCH_POLICY_UNSUPPORTED":
			code, message = remote.Code, "当前 Licentivo 不支持所选策略的 Launch 账号接入，请核对授权模式及服务端版本"
		case remote.Code == "LAUNCH_HTTPS_REQUIRED":
			code, message = remote.Code, "当前 Launch 地址使用 HTTP，但此 API Key 未允许 HTTP 站点接入。请在 Licentivo 的“开发与接入 → API Key”中打开此 Key 的“接入设置”，允许 HTTP，或为 Launch 配置 HTTPS"
		case remote.Code == "LAUNCH_TRUST_IMMUTABLE":
			code, message = remote.Code, "已有账号连接的站点地址或环境不能修改，请使用当前网站地址重新建立连接"
		case remote.Code == "LICENTIVO_RESPONSE_INVALID":
			code, message = remote.Code, "Licentivo 返回的数据格式无效，请检查服务端版本或代理响应"
		case remote.Status == 401:
			code, message = "LICENSE_KEY_INVALID", "Licentivo API Key 无效、已到期或已吊销，请检查并更新 Key"
		case remote.Status == 403:
			code, message = "LICENSE_KEY_FORBIDDEN", "Licentivo API Key 权限不足，请使用有效 Owner 创建的读写 Key"
		case errors.Is(remote, ports.ErrContractUnsupported):
			message = "Licentivo 未提供所需接口，请确认 Licentivo 服务端版本支持 Launch 账号连接"
		case remote.Message != "":
			message = remote.Message
		default:
			message = fmt.Sprintf("Licentivo 返回 HTTP %d，请稍后重试或联系支持", remote.Status)
		}
		if remote.Code != "" {
			message += "（Licentivo 错误码：" + remote.Code + "）"
		}
		if remote.RequestID != "" {
			message += "（请求编号：" + remote.RequestID + "）"
		}
	} else if errors.As(err, &transport) {
		code = transport.Reason
		if code == "LICENTIVO_TIMEOUT" {
			message = "连接 Licentivo 超时，请检查测试服务器的出站网络、DNS 和代理设置后重试"
		} else if code == "LICENTIVO_TLS_ERROR" {
			message = "Licentivo HTTPS 证书校验失败，请检查测试服务器的系统时间、受信证书和 HTTPS 代理"
		}
	} else if errors.Is(err, licentivo.ErrCredentials) {
		code, message = "LICENSE_KEY_INVALID", "Licentivo API Key 格式无效或权限不足，请检查并更新 Key"
	} else if errors.Is(err, licentivo.ErrConflict) {
		code, message = "LICENSE_CONNECTION_CONFLICT", "Licentivo 返回的连接参数与当前网站地址或环境不一致，或远端状态存在冲突，请核对后重新连接"
	} else if errors.Is(err, ports.ErrContractUnsupported) {
		message = "Licentivo 未提供所需接口，请确认服务端版本支持 Launch 账号连接"
	}
	return problem(400, code, stage+"失败："+message)
}

// A rejected mapping is not necessarily a policy version change. Preserve the
// adapter's sanitized remote code and request ID so operators can act on it.
func licentivoPlanMappingError(err error) error {
	detail := licentivoConnectionError("注册套餐授权映射", err).(*Error)
	var remote *licentivo.APIError
	var transport *licentivo.TransportError
	if errors.As(err, &remote) {
		if remote.Status == 409 {
			detail.Status = 409
			if remote.Code == "POLICY_VERSION_CONFLICT" {
				detail.Code = "LICENSE_POLICY_CHANGED"
			} else if remote.Code != "" {
				detail.Code = remote.Code
			}
		} else if remote.Status == 429 || remote.Status >= 500 {
			detail.Status = 503
		}
	} else if errors.As(err, &transport) || errors.Is(err, licentivo.ErrUnavailable) {
		detail.Status = 503
	} else if errors.Is(err, licentivo.ErrConflict) {
		detail.Status, detail.Code = 409, "LICENSE_POLICY_CHANGED"
	}
	return detail
}

// Deterministic customer/device rejections must not invite retries as an outage.
// Remote authentication failures still describe the merchant bridge, not the
// customer's website session. APIError diagnostics are sanitized by the adapter.
func licentivoDeviceError(err error) error {
	var remote *licentivo.APIError
	if !errors.As(err, &remote) || remote.Status < 400 || remote.Status >= 500 || remote.Status == 401 || remote.Status == 429 || remote.Code == "LICENTIVO_RESPONSE_INVALID" {
		return bridgePending()
	}
	code := remote.Code
	if code == "" {
		code = "DEVICE_OPERATION_REJECTED"
	}
	message := "授权服务拒绝此设备操作，请刷新设备列表后核对状态"
	switch code {
	case "IDEMPOTENCY_CONFLICT":
		message = "此操作编号已用于其他设备操作，请刷新列表后重新提交"
	case "SELF_RELEASE_LIMIT":
		message = "自助释放次数已用尽，请联系商家处理"
	case "NOT_FOUND":
		message = "未找到此授权连接，请刷新设备列表"
	}
	if remote.RequestID != "" {
		message += "（授权服务请求编号：" + remote.RequestID + "）"
	}
	return problem(remote.Status, code, message)
}

func (s *Service) recordLicentivoConnectionFailure(ctx context.Context, id string, failure error) error {
	if id == "" {
		return failure
	}
	var detail *Error
	if errors.As(failure, &detail) {
		if err := s.db.WithContext(ctx).Model(&storage.LicentivoConnection{}).Where("id = ?", id).Updates(map[string]any{"last_check_error": detail.Message, "last_checked_at": now()}).Error; err != nil {
			return ErrStorage
		}
	}
	return failure
}
