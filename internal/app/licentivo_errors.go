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
