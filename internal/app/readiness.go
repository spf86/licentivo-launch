package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"licentivo-launch/internal/storage"
)

type OperationalCheck struct {
	Key       string     `json:"key"`
	Label     string     `json:"label"`
	Status    string     `json:"status"`
	Ready     bool       `json:"ready"`
	Detail    string     `json:"detail"`
	Target    string     `json:"target,omitempty"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
}

type OperationalReadiness struct {
	Checks      []OperationalCheck `json:"checks"`
	ReadyCount  int                `json:"ready_count"`
	Total       int                `json:"total"`
	GeneratedAt time.Time          `json:"generated_at"`
}

func readinessCheck(key, label, status, detail, target string, checked *time.Time) OperationalCheck {
	return OperationalCheck{Key: key, Label: label, Status: status, Ready: status == "ready", Detail: detail, Target: target, CheckedAt: checked}
}

func (s *Service) checkMySQL(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return sqlDB.PingContext(checkCtx)
}

func (s *Service) checkLicentivoConnection(ctx context.Context) {
	var connection storage.LicentivoConnection
	if s.db.WithContext(ctx).Order("created_at,id").First(&connection).Error != nil {
		return
	}
	checked := now()
	result := map[string]any{"last_checked_at": checked, "last_check_error": nil}
	key, err := s.readSecret(ctx, connection.CredentialSecretID)
	if err == nil {
		var client LicentivoSetupProvider
		client, err = s.licentivoSetupProvider(string(key))
		if err == nil {
			_, err = client.Products(ctx)
		}
		if err == nil {
			_, err = client.Policies(ctx)
		}
		if err == nil {
			var capsErr error
			caps, capsErr := client.Capabilities(ctx)
			if capsErr != nil || caps.ValidateSales() != nil {
				err = errors.New("read/write capability is unavailable")
			}
		}
	}
	if err != nil {
		result["last_check_error"] = "Licentivo 读取或写入权限检查失败"
	}
	_ = s.db.WithContext(context.WithoutCancel(ctx)).Model(&storage.LicentivoConnection{}).Where("id = ?", connection.ID).Updates(result).Error
}

func (s *Service) checkDeepLConnection(ctx context.Context) {
	var settings storage.TranslationSettings
	if s.db.WithContext(ctx).First(&settings, "id = ?", 1).Error != nil {
		return
	}
	checked := now()
	changes := map[string]any{"last_checked_at": checked, "last_check_error": nil}
	key, err := s.readSecret(ctx, settings.CredentialSecretID)
	if err == nil {
		translator, providerErr := s.translationProvider(settings.Edition, string(key))
		if providerErr != nil {
			err = providerErr
		} else {
			checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			usage, usageErr := translator.Usage(checkCtx)
			cancel()
			err = usageErr
			if usageErr == nil {
				changes["character_count"], changes["character_limit"] = usage.CharacterCount, usage.CharacterLimit
			}
		}
	}
	if err != nil {
		changes["last_check_error"] = "DeepL 额度接口检查失败"
	}
	_ = s.db.WithContext(context.WithoutCancel(ctx)).Model(&storage.TranslationSettings{}).Where("id = ?", 1).Updates(changes).Error
}

func (s *Service) checkPaymentConnections(ctx context.Context) {
	var accounts []storage.PaymentAccount
	if s.db.WithContext(ctx).Where("enabled = true").Find(&accounts).Error != nil {
		return
	}
	for _, account := range accounts {
		checked := now()
		changes := map[string]any{"last_checked_at": checked, "last_check_error": nil}
		provider, err := s.paymentProvider(ctx, account)
		if err == nil {
			if verifier, ok := provider.(interface{ VerifyAccount(context.Context) error }); ok {
				checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
				err = verifier.VerifyAccount(checkCtx)
				cancel()
			} else {
				err = errors.New("account verification unsupported")
			}
		}
		if err != nil {
			changes["last_check_error"] = "支付账号连接检查失败"
		}
		_ = s.db.WithContext(context.WithoutCancel(ctx)).Model(&storage.PaymentAccount{}).Where("id = ?", account.ID).Updates(changes).Error
	}
}

func (s *Service) OperationalReadiness(ctx context.Context, p Principal, run string) (OperationalReadiness, error) {
	if p.Role == "customer" {
		return OperationalReadiness{}, ErrForbidden
	}
	if run != "" && !p.IsAdmin() {
		return OperationalReadiness{}, ErrForbidden
	}
	switch run {
	case "":
	case "mysql":
		_ = s.checkMySQL(ctx)
	case "mail":
		_, _ = s.CheckMail(ctx, p, MailCheckInput{To: p.User.Email})
	case "licentivo":
		s.checkLicentivoConnection(ctx)
	case "deepl":
		s.checkDeepLConnection(ctx)
	case "payments":
		s.checkPaymentConnections(ctx)
	default:
		return OperationalReadiness{}, ErrInvalid
	}

	checks := make([]OperationalCheck, 0, 9)
	at := now()
	if err := s.checkMySQL(ctx); err != nil {
		checks = append(checks, readinessCheck("mysql", "MySQL 数据库", "failed", "无法执行数据库心跳，请检查网络和 MySQL 服务", "#/admin/settings", &at))
	} else {
		checks = append(checks, readinessCheck("mysql", "MySQL 数据库", "ready", "数据库连接和查询正常", "#/admin/settings", &at))
	}

	var instanceSettings storage.InstanceSettings
	tlsStatus, tlsDetail := "warning", "网站当前使用 HTTP；正式开放登录和收款前请配置证书并切换到 HTTPS"
	var tlsCheckedAt *time.Time
	if err := s.db.WithContext(ctx).First(&instanceSettings, "id = ?", 1).Error; err != nil {
		tlsStatus, tlsDetail = "failed", "无法读取 SSL/TLS 设置"
	} else if tlsView, err := s.tlsSettingsView(instanceSettings); err != nil {
		tlsStatus, tlsDetail = "failed", "无法验证已保存的 SSL/TLS 证书信息"
	} else {
		tlsCheckedAt = tlsView.UpdatedAt
		if tlsView.ExpectedHostname != "" {
			switch {
			case !tlsView.ListenerEnabled:
				tlsStatus, tlsDetail = "warning", "公开地址使用 HTTPS，但由外部反向代理终止 TLS；请在代理端确认有效证书"
			case !tlsView.Configured:
				tlsStatus, tlsDetail = "failed", "HTTPS 监听已启用，但还没有保存服务器证书"
			case tlsView.Expired:
				tlsStatus, tlsDetail = "failed", "已保存的 SSL/TLS 证书已经过期"
			case tlsView.ExpiresWithin30Days:
				tlsStatus, tlsDetail = "warning", "SSL/TLS 证书将在 30 天内到期，请及时更换"
			default:
				tlsStatus, tlsDetail = "ready", "HTTPS 已启用，服务器证书有效且域名匹配"
			}
		}
	}
	checks = append(checks, readinessCheck("tls", "HTTPS 与 SSL 证书", tlsStatus, tlsDetail, "#/admin/settings", tlsCheckedAt))

	var runtime storage.RuntimeStatus
	_ = s.db.WithContext(ctx).First(&runtime, "id = ?", 1).Error
	workerReady := runtime.CommerceHeartbeat != nil && runtime.MailHeartbeat != nil && at.Sub(*runtime.CommerceHeartbeat) < time.Minute && at.Sub(*runtime.MailHeartbeat) < time.Minute
	var queued, attention int64
	_ = s.db.WithContext(ctx).Model(&storage.SyncTask{}).Where("status IN ('pending','running','retrying')").Count(&queued).Error
	_ = s.db.WithContext(ctx).Model(&storage.SyncTask{}).Where("status = 'manual_review'").Count(&attention).Error
	workerStatus := "ready"
	workerDetail := fmt.Sprintf("授权队列 %d 项，需人工处理 %d 项；邮件与交易后台任务正在运行", queued, attention)
	if !workerReady {
		workerStatus, workerDetail = "failed", "后台任务心跳中断，请检查 Launch 进程"
	} else if attention > 0 {
		workerStatus, workerDetail = "warning", fmt.Sprintf("后台任务正常，但有 %d 项授权任务需要人工处理", attention)
	}
	checks = append(checks, readinessCheck("workers", "后台任务", workerStatus, workerDetail, "#/admin/operations", runtime.CommerceHeartbeat))

	backupStatus, backupDetail := "warning", "尚未记录成功备份；请在服务器运行 launch backup"
	if runtime.LastBackupAt != nil {
		age := at.Sub(*runtime.LastBackupAt)
		backupStatus, backupDetail = "ready", "最近一次加密备份已成功完成"
		if age > 7*24*time.Hour {
			backupStatus, backupDetail = "warning", "最近一次成功备份已超过 7 天"
		}
	}
	checks = append(checks, readinessCheck("backup", "加密备份", backupStatus, backupDetail, "#/admin/settings", runtime.LastBackupAt))

	var mail storage.MailSettings
	mailErr := s.db.WithContext(ctx).First(&mail, "id = ? AND enabled = true", mailSettingsID).Error
	mailStatus, mailDetail := "ready", "邮件未启用，客户可直接注册、登录和购买"
	if mailErr == nil {
		mailStatus, mailDetail = "warning", "邮件已启用，请发送测试邮件确认发信正常"
	}
	if mailErr == nil && mail.LastCheckedAt != nil {
		mailStatus, mailDetail = "ready", "测试邮件已由 SMTP 服务器接收"
		if mail.LastCheckError != nil {
			mailStatus, mailDetail = "failed", *mail.LastCheckError
		} else if at.Sub(*mail.LastCheckedAt) > 7*24*time.Hour {
			mailStatus, mailDetail = "warning", "上次成功发信测试已超过 7 天"
		}
	}
	checks = append(checks, readinessCheck("mail", "邮件（可选）", mailStatus, mailDetail, "#/admin/settings/mail", mail.LastCheckedAt))

	var connection storage.LicentivoConnection
	licenseStatus, licenseDetail := "warning", "尚未连接 Licentivo"
	if err := s.db.WithContext(ctx).Order("created_at,id").First(&connection).Error; err == nil {
		licenseStatus, licenseDetail = "ready", "产品、策略和读写授权能力检查通过"
		if connection.ContractVersion == nil {
			licenseStatus, licenseDetail = "failed", "API Key 缺少销售授权所需的读写权限"
		} else if connection.LastCheckError != nil {
			licenseStatus, licenseDetail = "failed", *connection.LastCheckError
		} else if connection.LastCheckedAt == nil {
			licenseStatus, licenseDetail = "warning", "连接已保存，请执行一次读写权限检查"
		}
	}
	checks = append(checks, readinessCheck("licentivo", "Licentivo 读写权限", licenseStatus, licenseDetail, "#/admin/settings", connection.LastCheckedAt))

	var translation storage.TranslationSettings
	deepLStatus, deepLDetail := "warning", "尚未连接 DeepL"
	if err := s.db.WithContext(ctx).First(&translation, "id = ?", 1).Error; err == nil {
		deepLStatus, deepLDetail = "ready", fmt.Sprintf("DeepL 可用，本期已使用 %d / %d 字符", translation.CharacterCount, translation.CharacterLimit)
		if translation.LastCheckError != nil {
			deepLStatus, deepLDetail = "failed", *translation.LastCheckError
		} else if translation.LastCheckedAt == nil {
			deepLStatus, deepLDetail = "warning", "DeepL 已配置，请执行一次额度接口检查"
		}
	}
	checks = append(checks, readinessCheck("deepl", "DeepL 翻译", deepLStatus, deepLDetail, "#/admin/settings", translation.LastCheckedAt))

	var accounts []storage.PaymentAccount
	_ = s.db.WithContext(ctx).Where("enabled = true").Order("environment,provider").Find(&accounts).Error
	if len(accounts) == 0 {
		checks = append(checks, readinessCheck("payments", "支付与退款通知", "warning", "尚未配置收款账号和 Webhook", "#/admin/settings/payments", nil))
	} else {
		status, detail := "ready", "支付账号可用，Webhook 已收到并验证事件"
		var latest *time.Time
		for _, account := range accounts {
			if account.LastCheckError != nil {
				status, detail = "failed", account.Provider+" 支付账号连接检查失败"
			} else if account.LastCheckedAt == nil && status != "failed" {
				status, detail = "warning", "支付账号已保存，但还没有执行连接检查"
			}
			var event storage.PaymentEvent
			err := s.db.WithContext(ctx).Where("account_id = ?", account.ID).Order("received_at DESC").First(&event).Error
			if errors.Is(err, gorm.ErrRecordNotFound) && status != "failed" {
				status, detail = "warning", "支付账号已验证；至少一个 Webhook 端点还没有收到测试事件"
			} else if err == nil {
				when := event.ReceivedAt
				if latest == nil || when.After(*latest) {
					latest = &when
				}
				if event.Status == "review" {
					status, detail = "failed", "最近的支付 Webhook 事件需要人工核对"
				}
			}
		}
		checks = append(checks, readinessCheck("payments", "支付与退款通知", status, detail, "#/admin/settings/payments", latest))
	}

	report := OperationalReadiness{Checks: checks, Total: len(checks), GeneratedAt: at}
	for _, check := range checks {
		if check.Ready {
			report.ReadyCount++
		}
	}
	return report, nil
}
