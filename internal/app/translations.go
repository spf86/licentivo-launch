package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/adapters/deepl"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

const defaultSourceLocale = "zh-CN"

type LocaleOption struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

var localeOptions = []LocaleOption{
	{Code: "zh-CN", Name: "简体中文"},
	{Code: "en", Name: "English"},
	{Code: "ja", Name: "日本語"},
	{Code: "ko", Name: "한국어"},
	{Code: "de", Name: "Deutsch"},
	{Code: "fr", Name: "Français"},
	{Code: "es", Name: "Español"},
	{Code: "pt-BR", Name: "Português (Brasil)"},
	{Code: "it", Name: "Italiano"},
	{Code: "ru", Name: "Русский"},
}

func supportedLocales() []LocaleOption {
	return append([]LocaleOption(nil), localeOptions...)
}

func canonicalLocale(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ",")[0]))
	if semi := strings.IndexByte(value, ';'); semi >= 0 {
		value = value[:semi]
	}
	switch {
	case value == "zh" || strings.HasPrefix(value, "zh-cn") || strings.HasPrefix(value, "zh-hans"):
		return "zh-CN"
	case value == "en" || strings.HasPrefix(value, "en-"):
		return "en"
	case value == "ja" || strings.HasPrefix(value, "ja-"):
		return "ja"
	case value == "ko" || strings.HasPrefix(value, "ko-"):
		return "ko"
	case value == "de" || strings.HasPrefix(value, "de-"):
		return "de"
	case value == "fr" || strings.HasPrefix(value, "fr-"):
		return "fr"
	case value == "es" || strings.HasPrefix(value, "es-"):
		return "es"
	case value == "pt" || strings.HasPrefix(value, "pt-"):
		return "pt-BR"
	case value == "it" || strings.HasPrefix(value, "it-"):
		return "it"
	case value == "ru" || strings.HasPrefix(value, "ru-"):
		return "ru"
	default:
		return ""
	}
}

func deeplLanguage(locale string, target bool) string {
	switch locale {
	case "zh-CN":
		if target {
			return "ZH-HANS"
		}
		return "ZH"
	case "en":
		if target {
			return "EN-US"
		}
		return "EN"
	case "pt-BR":
		if target {
			return "PT-BR"
		}
		return "PT"
	default:
		return strings.ToUpper(locale)
	}
}

type TranslationPayload struct {
	Name                 string            `json:"name,omitempty"`
	Tagline              string            `json:"tagline,omitempty"`
	Description          string            `json:"description,omitempty"`
	Summary              string            `json:"summary,omitempty"`
	Features             []string          `json:"features,omitempty"`
	Requirements         string            `json:"requirements,omitempty"`
	PlatformRequirements map[string]string `json:"platform_requirements,omitempty"`
	InstallationGuide    string            `json:"installation_guide,omitempty"`
	UpgradeGuide         string            `json:"upgrade_guide,omitempty"`
	UninstallGuide       string            `json:"uninstall_guide,omitempty"`
	TroubleshootingGuide string            `json:"troubleshooting_guide,omitempty"`
	TermsOfService       string            `json:"terms_of_service,omitempty"`
	PrivacyPolicy        string            `json:"privacy_policy,omitempty"`
	RefundPolicy         string            `json:"refund_policy,omitempty"`
	InvoiceGuide         string            `json:"invoice_guide,omitempty"`
	LegalTaxNotice       string            `json:"legal_tax_notice,omitempty"`
	OfflineSummary       string            `json:"offline_summary,omitempty"`
}

var siteUIStrings = map[string]string{
	"auth.register_direct":             "使用邮箱和密码创建账号，注册后即可登录。",
	"auth.mail_unavailable":            "本站未启用邮件，请联系商家找回密码。",
	"auth.password_help":               "密码至少 12 个字符。",
	"toast.registration_ready":         "注册已完成，请使用邮箱和密码登录",
	"account.email_optional":           "无需邮箱验证",
	"account.email_change_unavailable": "如需修改邮箱，请联系商家。",
	"language.label":                   "语言",
	"nav.products":                     "产品",
	"nav.docs":                         "产品文档",
	"nav.account":                      "我的账号",
	"nav.admin":                        "管理后台",
	"nav.logout":                       "退出",
	"nav.login":                        "登录",
	"nav.register":                     "创建账号",
	"common.loading":                   "正在加载…",
	"common.test":                      "测试",
	"common.permanent":                 "永久",
	"common.download":                  "下载",
	"common.view_product":              "查看产品",
	"common.account_center":            "进入账号中心",
	"common.back_product":              "返回产品介绍",
	"common.back_account":              "返回账号中心",
	"common.days":                      "天",
	"common.months":                    "个月",
	"common.years":                     "年",
	"common.device":                    "台设备",
	"common.seat":                      "个同时使用席位",
	"common.times":                     "次",
	"common.time":                      "时间",
	"common.amount":                    "金额",
	"common.action":                    "操作",
	"common.status":                    "状态",
	"common.type":                      "类型",
	"common.created":                   "创建时间",
	"common.expires":                   "到期时间",
	"common.price_pending":             "价格待确认",
	"common.requested_at":              "申请时间",
	"common.confirmed_at":              "退款确认",
	"section.products":                 "产品",
	"section.plans":                    "套餐",
	"section.download":                 "下载",
	"section.account":                  "账号",
	"section.order":                    "订单",
	"section.continue_plan":            "继续使用套餐",
	"section.review_change":            "确认变更",
	"section.devices":                  "你的设备",
	"section.checkout":                 "结账",
	"home.product_fallback":            "产品",
	"home.card_fallback":               "查看产品功能、套餐与下载信息。",
	"home.preparing":                   "产品正在准备",
	"home.preparing_body":              "站长发布产品后会显示在这里。",
	"home.test_notice":                 "测试环境：支付使用沙箱，不会产生真实扣款。",
	"home.software_start":              "软件，从这里开始",
	"home.summary_fallback":            "产品正在准备中，欢迎稍后访问。",
	"home.login_title":                 "登录账号，开始使用",
	"home.login_body":                  "你的购买记录和软件权益，统一保存在商家账号中。",
	"home.all_products":                "全部产品",
	"home.product_count":               "个产品",
	"home.choose_product":              "选择软件产品",
	"home.choose_product_body":         "每个产品都有独立的介绍、安装包和销售套餐。进入产品后再选择对应套餐。",
	"home.product_scope":               "独立产品 · 专属套餐、下载与授权",
	"product.about":                    "关于产品",
	"product.position":                 "当前位置",
	"product.current":                  "当前产品",
	"product.independent_scope":        "下方介绍、下载和套餐均只属于此产品。",
	"product.requirements":             "系统要求",
	"product.help":                     "安装与使用帮助",
	"product.policies":                 "服务政策与购买说明",
	"product.installation":             "安装指南",
	"product.upgrade":                  "升级指南",
	"product.uninstall":                "卸载指南",
	"product.troubleshooting":          "故障排查",
	"product.terms":                    "服务条款",
	"product.privacy":                  "隐私政策",
	"product.refunds":                  "退款规则",
	"product.invoices":                 "开票流程",
	"product.legal_tax":                "法律与税务说明",
	"docs.title":                       "产品文档",
	"docs.subtitle":                    "安装、更新、故障排查与购买政策集中在这里。",
	"docs.contents":                    "文档目录",
	"docs.general_requirements":        "通用要求",
	"docs.need_help":                   "需要安装帮助？",
	"docs.need_help_body":              "查看安装、升级、卸载和故障排查文档。",
	"docs.empty":                       "文档正在准备",
	"docs.empty_body":                  "管理员发布文档后会显示在这里。",
	"docs.not_found":                   "没有找到产品文档",
	"docs.not_found_body":              "请返回产品目录选择其他产品。",
	"docs.center_title":                "产品文档中心",
	"docs.center_subtitle":             "请选择软件产品。每个产品的安装、使用、故障排查和政策文档相互独立。",
	"docs.help_count":                  "篇使用文档",
	"docs.policy_count":                "篇政策文档",
	"docs.open_product":                "查看该产品文档",
	"download.file_size":               "文件大小",
	"download.checksum":                "SHA-256 校验值",
	"download.all_platforms":           "通用版本",
	"plans.title":                      "选择适合你的套餐",
	"plans.for_product":                "选择 {product} 套餐",
	"plans.product_scope":              "以下套餐只适用于当前产品。",
	"plans.one_year":                   "1 年",
	"plans.month_count":                "{count} 个月",
	"plans.no_auto":                    "固定期限购买不会自动扣款",
	"plans.choose":                     "选择套餐",
	"plans.preview":                    "测试预览套餐",
	"plans.soon":                       "套餐即将开放",
	"plans.soon_body":                  "商家正在准备销售套餐，可先创建账号。",
	"plans.summary_fallback":           "购买后立即获得对应期限的软件使用权。",
	"plans.offline_limited":            "支持有限离线使用",
	"plans.offline_permanent":          "支持长期离线使用",
	"plans.self_release":               "可自助解绑设备",
	"download.none":                    "暂未提供下载",
	"download.none_body":               "请稍后再来查看当前产品版本。",
	"download.current":                 "当前版本",
	"download.requirements_fallback":   "请在下载后查看随附的系统要求。",
	"checkout.policy_note":             "继续支付即表示你已阅读",
	"checkout.policy_and":              "与",
	"checkout.choose_period":           "选择购买期限",
	"auth.welcome":                     "欢迎回来",
	"auth.login_body":                  "登录商家的账号，查看产品与权益",
	"auth.create":                      "创建账号",
	"auth.register_body":               "使用邮箱注册，购买前需要完成邮箱验证",
	"auth.forgot":                      "找回密码",
	"auth.forgot_body":                 "我们会向有效的账号邮箱发送重置链接",
	"auth.send_reset":                  "发送重置邮件",
	"auth.register":                    "注册",
	"auth.email":                       "邮箱",
	"auth.password":                    "密码",
	"auth.forgot_link":                 "忘记密码",
	"auth.back_login":                  "返回登录",
	"auth.verify_title":                "确认邮箱",
	"auth.verify_body":                 "确认后即可使用该邮箱关联的账号。",
	"auth.verify_button":               "确认验证",
	"auth.reset_title":                 "设置新密码",
	"auth.reset_body":                  "设置新密码后，所有旧登录会话会失效。",
	"auth.save_password":               "保存新密码",
	"account.title":                    "我的账号",
	"account.verified":                 "已验证",
	"account.unverified":               "待验证",
	"account.resend":                   "重发验证邮件",
	"account.entitlements":             "软件权益",
	"account.active":                   "已开通",
	"account.pending":                  "授权处理中",
	"account.expired":                  "已到期",
	"account.suspended":                "已暂停",
	"account.valid_until":              "有效期至",
	"account.synced":                   "授权已同步",
	"account.syncing":                  "授权正在同步",
	"account.none":                     "你的软件权益",
	"account.none_body":                "当前还没有已购产品。购买后，权益和使用期限会显示在这里。",
	"account.orders":                   "订单记录",
	"account.no_orders":                "暂无订单",
	"account.no_orders_body":           "你的购买记录会显示在这里。",
	"account.change_password":          "修改密码",
	"account.current_password":         "当前密码",
	"account.new_password":             "新密码",
	"account.save_relogin":             "保存并重新登录",
	"account.change_email":             "修改邮箱",
	"account.new_email":                "新邮箱",
	"account.send_confirm":             "发送确认邮件",
	"account.sessions":                 "登录会话",
	"account.website":                  "网站",
	"account.software":                 "软件",
	"account.current_session":          "当前会话",
	"account.logout_session":           "退出此会话",
	"account.center":                   "客户中心",
	"account.products_count":           "软件权益",
	"account.orders_count":             "订单",
	"account.refunds_pending":          "退款处理中",
	"account.invoices_pending":         "发票处理中",
	"account.downloads_title":          "下载、授权与设备",
	"account.my_software":              "我的软件",
	"account.license_validity":         "授权有效期",
	"account.license_sync":             "授权同步",
	"account.current_devices":          "当前设备",
	"account.device_unavailable":       "暂不可用",
	"account.device_unit":              "台/席位",
	"account.download":                 "下载",
	"account.product_help":             "产品说明",
	"account.orders_invoices":          "订单、续费与发票",
	"account.product_plan":             "产品与套餐",
	"account.order_time":               "订单时间",
	"account.amount_period":            "金额与期限",
	"account.invoice":                  "发票",
	"account.request_invoice":          "申请发票",
	"account.download_invoice":         "下载发票",
	"account.invoice_waiting":          "等待开票",
	"account.invoice_issued":           "已开具",
	"account.invoice_rejected":         "未通过",
	"account.refund_progress":          "退款进度",
	"account.no_refunds":               "暂无退款申请",
	"account.no_refunds_body":          "申请退款后，审核和原渠道退款进度会显示在这里。",
	"account.request_time":             "申请时间",
	"account.reason":                   "原因",
	"account.security_sessions":        "账号安全与登录会话",
	"invoice.request_title":            "申请发票",
	"invoice.type":                     "发票类型",
	"invoice.personal":                 "个人",
	"invoice.company":                  "企业",
	"invoice.title":                    "发票抬头",
	"invoice.tax_id":                   "纳税人识别号（企业必填）",
	"invoice.email":                    "接收邮箱",
	"invoice.submit":                   "提交开票申请",
	"toast.invoice_requested":          "开票申请已提交，可在客户中心查看进度",
	"common.close":                     "关闭",
	"common.cancel":                    "取消",
	"account.order_waiting":            "等待付款",
	"account.order_paid":               "已付款",
	"account.order_closed":             "已关闭",
	"account.order_refunded":           "已退款",
	"account.order_refunding":          "退款中",
	"action.purchase":                  "购买",
	"action.renew":                     "续费",
	"action.upgrade":                   "升级",
	"checkout.title":                   "确认购买",
	"checkout.fixed_notice":            "从成功收款时起算，不会自动扣款。",
	"checkout.perpetual_notice":        "永久使用权益，维护与版本范围以套餐约定为准。",
	"checkout.test_notice":             "当前为测试收款，不会真实扣款。",
	"checkout.provider":                "支付渠道",
	"checkout.pay":                     "前往支付",
	"checkout.verify":                  "先验证邮箱",
	"checkout.verify_body":             "验证邮箱后即可购买产品。",
	"checkout.unavailable":             "该套餐暂未开放购买",
	"checkout.unavailable_body":        "请返回产品页查看当前可用套餐。",
	"order.active":                     "软件权益已开通",
	"order.processing":                 "已付款，授权处理中",
	"order.refunded":                   "订单已退款",
	"order.waiting":                    "等待付款确认",
	"order.amount":                     "订单金额",
	"order.test":                       "测试订单",
	"order.created":                    "创建于",
	"order.paid_at":                    "收款于",
	"order.active_body":                "请在软件中登录同一账号，即可自动取得自己的授权。",
	"order.processing_body":            "商家已确认收款，授权任务会继续处理。无需再次付款。",
	"order.unpaid_body":                "若已经付款，请查询原订单结果，避免重复下单。",
	"order.continue":                   "继续原订单付款",
	"order.check":                      "查询付款结果",
	"order.refund_progress":            "退款进度",
	"order.request_refund":             "申请整笔退款",
	"order.refund_body":                "商家审核后按原支付渠道退款。申请不会立即停止权益；成功退款后按剩余已付覆盖重新计算。",
	"order.refund_reason":              "退款原因",
	"order.submit_refund":              "提交退款申请",
	"refund.review":                    "需商家核实",
	"refund.requested":                 "等待审核",
	"refund.pending":                   "原渠道退款处理中",
	"refund.unknown":                   "退款结果核实中",
	"refund.succeeded":                 "已确认退款",
	"refund.failed":                    "渠道退回，需商家处理",
	"account.manage_title":             "续费、升级与设备",
	"account.manage_body":              "续费保持原合同锚点；升级会先计算补差价，再确认付款。",
	"account.test_rights":              "测试权益",
	"account.software_rights":          "软件权益",
	"account.renew_upgrade":            "续费或升级",
	"account.manage_devices":           "管理设备",
	"change.title":                     "续费或升级",
	"change.body":                      "选择操作与目标套餐，系统会核对当前已付权益并给出报价。",
	"change.operation":                 "操作",
	"change.renew":                     "续费当前套餐",
	"change.upgrade":                   "升级套餐",
	"change.target":                    "目标套餐",
	"change.quote":                     "计算报价",
	"change.review_renew":              "确认续费",
	"change.review_upgrade":            "确认升级",
	"change.period_starts":             "新增已付周期开始",
	"change.rights_start":              "新权益生效",
	"change.contract_end":              "合同截止",
	"change.time_display":              "时间显示为",
	"change.quote_expires":             "报价有效至",
	"change.upgrade_notice":            "升级保留当前已付周期截止及原使用记录；后续已付周期按各订单约定履行。",
	"change.renew_notice":              "有效期内续费衔接原期末；若原周期在收款前已到期，则从实际收款时重新起算。",
	"change.confirm_pay":               "确认并前往支付",
	"devices.title":                    "设备与席位",
	"devices.remaining_a":              "最近 30 天剩余",
	"devices.remaining_b":              "次自助释放。重试同一操作只占用一次额度。",
	"devices.body":                     "释放后，设备下一次成功联机将按最新状态验证；已签发的离线许可仍受原离线期限约束。",
	"devices.unnamed":                  "未命名设备",
	"devices.active":                   "使用中",
	"devices.released":                 "已释放",
	"devices.last_seen":                "最近联机",
	"devices.lease_end":                "席位截止",
	"devices.release":                  "释放这台设备",
	"devices.none":                     "还没有设备",
	"devices.none_body":                "在软件中登录并激活后，会在这里显示。",
	"devices.recent":                   "近期释放操作",
	"devices.waiting":                  "正在等待处理",
	"devices.releasing":                "正在释放",
	"devices.done":                     "已释放",
	"devices.review":                   "需联系商家核实",
	"toast.device_queued":              "设备释放已排队，重试不会重复扣次数",
	"toast.refund_submitted":           "退款申请已提交，等待商家审核",
	"toast.registration_sent":          "如信息有效，验证邮件会发送至该地址",
	"toast.reset_sent":                 "如账号有效，重置邮件会发送至该地址",
	"toast.action_complete":            "操作已完成，请登录",
	"toast.password_changed":           "密码已修改，请重新登录",
	"toast.email_confirmation":         "请在新邮箱中确认变更",
	"toast.verification_resent":        "如信息有效，验证邮件会重新发送",
	"toast.session_closed":             "会话已退出",
	"toast.preview_only":               "当前为测试预览，购买功能尚未开放",
	"error.link_closed":                "链接已关闭",
	"error.link_closed_body":           "请重新打开邮件中的链接。",
	"error.service_preparing":          "服务正在准备",
	"translation.preparing":            "所选语言的内容正在生成，当前显示原文。",
	"footer.local":                     "客户资料由商家保存",
}

func brandTranslationPayload(brand Brand) TranslationPayload {
	return TranslationPayload{Name: brand.Name, Tagline: brand.Tagline, Description: brand.Description}
}

func productTranslationPayload(product ProductView) TranslationPayload {
	platformRequirements := make(map[string]string, len(product.Content.PlatformRequirements))
	for platform, value := range product.Content.PlatformRequirements {
		platformRequirements[platform] = value
	}
	return TranslationPayload{
		Name: product.Name, Summary: product.Content.Summary, Description: product.Content.Description,
		Features: append([]string(nil), product.Content.Features...), Requirements: product.Content.Requirements,
		PlatformRequirements: platformRequirements, InstallationGuide: product.Content.InstallationGuide,
		UpgradeGuide: product.Content.UpgradeGuide, UninstallGuide: product.Content.UninstallGuide,
		TroubleshootingGuide: product.Content.TroubleshootingGuide, TermsOfService: product.Content.TermsOfService,
		PrivacyPolicy: product.Content.PrivacyPolicy, RefundPolicy: product.Content.RefundPolicy,
		InvoiceGuide: product.Content.InvoiceGuide, LegalTaxNotice: product.Content.LegalTaxNotice,
	}
}

func planTranslationPayload(plan PlanView) TranslationPayload {
	return TranslationPayload{Name: plan.Name, Summary: plan.Summary, Features: append([]string(nil), plan.Highlights...)}
}

func requestedContentLocale(tx *gorm.DB, requested string) (string, string) {
	source := sourceLocale(tx)
	locale := canonicalLocale(requested)
	if locale == "" {
		locale = source
	}
	return source, locale
}

func (s *Service) localizedProductView(ctx context.Context, view ProductView, locale, source string) (ProductView, bool) {
	if locale == source {
		return view, true
	}
	translated, ok := s.localizedPayload(ctx, "product", view.ID, locale, productTranslationPayload(view))
	if !ok {
		return view, false
	}
	view.Name, view.Content.Summary, view.Content.Description = translated.Name, translated.Summary, translated.Description
	view.Content.Features, view.Content.Requirements = translated.Features, translated.Requirements
	view.Content.PlatformRequirements = translated.PlatformRequirements
	view.Content.InstallationGuide, view.Content.UpgradeGuide = translated.InstallationGuide, translated.UpgradeGuide
	view.Content.UninstallGuide, view.Content.TroubleshootingGuide = translated.UninstallGuide, translated.TroubleshootingGuide
	view.Content.TermsOfService, view.Content.PrivacyPolicy = translated.TermsOfService, translated.PrivacyPolicy
	view.Content.RefundPolicy, view.Content.InvoiceGuide = translated.RefundPolicy, translated.InvoiceGuide
	view.Content.LegalTaxNotice = translated.LegalTaxNotice
	return view, true
}

func (s *Service) localizedPlanView(ctx context.Context, view PlanView, locale, source string) (PlanView, bool) {
	if locale == source {
		return view, true
	}
	translated, ok := s.localizedPayload(ctx, "plan_version", view.VersionID, locale, planTranslationPayload(view))
	if !ok {
		return view, false
	}
	view.Name, view.Summary, view.Highlights = translated.Name, translated.Summary, translated.Features
	return view, true
}

func sourceLocale(tx *gorm.DB) string {
	var settings storage.TranslationSettings
	if tx.First(&settings, "id = ?", 1).Error == nil && canonicalLocale(settings.SourceLocale) != "" {
		return canonicalLocale(settings.SourceLocale)
	}
	return defaultSourceLocale
}

func (s *Service) enqueueTranslation(tx *gorm.DB, resourceType, resourceID, locale string, payload TranslationPayload, force bool) error {
	raw := marshal(payload)
	hash := sha256.Sum256(raw)
	var current storage.TranslationTask
	err := tx.First(&current, "resource_type = ? AND resource_id = ?", resourceType, resourceID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	unchanged := err == nil && current.SourceLocale == locale && bytes.Equal(current.SourceHash, hash[:])
	if unchanged && current.Status == "succeeded" && !force {
		return nil
	}
	if !unchanged || force {
		if err := tx.Where("resource_type = ? AND resource_id = ?", resourceType, resourceID).Delete(&storage.ContentTranslation{}).Error; err != nil {
			return err
		}
	}
	id := current.ID
	if id == "" {
		var idErr error
		id, idErr = newID()
		if idErr != nil {
			return idErr
		}
	}
	task := storage.TranslationTask{ID: id, ResourceType: resourceType, ResourceID: resourceID, SourceLocale: locale, SourceHash: hash[:], SourceJSON: raw, Status: "pending", AvailableAt: now()}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "resource_type"}, {Name: "resource_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"source_locale": locale, "source_hash": hash[:], "source_json": raw, "status": "pending", "attempts": 0,
			"available_at": now(), "lease_token": nil, "lease_until": nil, "last_error_code": nil,
		}),
	}).Create(&task).Error
}

func (s *Service) enqueuePlanVersions(tx *gorm.DB, plan storage.Plan, locale string, force bool) error {
	var versions []storage.PlanVersion
	if err := tx.Where("plan_id = ?", plan.ID).Order("version").Find(&versions).Error; err != nil {
		return err
	}
	for _, version := range versions {
		if err := s.enqueueTranslation(tx, "plan_version", version.ID, locale, planTranslationPayload(planView(plan, version)), force); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) enqueueAllTranslations(tx *gorm.DB, locale string, force bool) error {
	var instance storage.InstanceSettings
	if err := tx.First(&instance, "id = ?", 1).Error; err != nil {
		return err
	}
	var brand Brand
	if json.Unmarshal(instance.Brand, &brand) != nil {
		return ErrStorage
	}
	if err := s.enqueueTranslation(tx, "brand", "1", locale, brandTranslationPayload(brand), force); err != nil {
		return err
	}
	var products []storage.Product
	if err := tx.Find(&products).Error; err != nil {
		return err
	}
	for _, product := range products {
		var content ProductContent
		if json.Unmarshal(product.Content, &content) != nil {
			return ErrStorage
		}
		view := ProductView{ID: product.ID, Name: product.Name, Slug: product.Slug, Status: product.Status, Content: content}
		if err := s.enqueueTranslation(tx, "product", product.ID, locale, productTranslationPayload(view), force); err != nil {
			return err
		}
	}
	var plans []storage.Plan
	if err := tx.Find(&plans).Error; err != nil {
		return err
	}
	for _, plan := range plans {
		if err := s.enqueuePlanVersions(tx, plan, locale, force); err != nil {
			return err
		}
	}
	return nil
}

type TranslationSettingsInput struct {
	Edition      string `json:"edition"`
	SourceLocale string `json:"source_locale"`
	APIKey       string `json:"api_key,omitempty"`
}

type TranslationStatus struct {
	Configured     bool           `json:"configured"`
	Provider       string         `json:"provider"`
	Edition        string         `json:"edition"`
	SourceLocale   string         `json:"source_locale"`
	Supported      []LocaleOption `json:"supported_locales"`
	CharacterCount uint64         `json:"character_count"`
	CharacterLimit uint64         `json:"character_limit"`
	LastCheckedAt  *time.Time     `json:"last_checked_at,omitempty"`
	Pending        int64          `json:"pending"`
	Completed      int64          `json:"completed"`
	NeedsAttention int64          `json:"needs_attention"`
	Total          int64          `json:"total"`
}

func (s *Service) translationProvider(edition, key string) (ports.Translator, error) {
	if s.config.TranslationFactory != nil {
		return s.config.TranslationFactory(edition, key)
	}
	return deepl.New(edition, key)
}

func (s *Service) TranslationStatus(ctx context.Context, p Principal) (TranslationStatus, error) {
	if !p.CanManage() {
		return TranslationStatus{}, ErrForbidden
	}
	view := TranslationStatus{Provider: "deepl", Edition: "free", SourceLocale: defaultSourceLocale, Supported: supportedLocales()}
	var settings storage.TranslationSettings
	err := s.db.WithContext(ctx).First(&settings, "id = ?", 1).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return view, ErrStorage
	}
	if err == nil {
		view.Configured, view.Edition, view.SourceLocale = true, settings.Edition, settings.SourceLocale
		view.CharacterCount, view.CharacterLimit, view.LastCheckedAt = settings.CharacterCount, settings.CharacterLimit, settings.LastCheckedAt
	}
	var rows []struct {
		Status string
		Count  int64
	}
	if err := s.db.WithContext(ctx).Model(&storage.TranslationTask{}).Where("resource_type <> ?", "site_ui").Select("status, count(*) AS count").Group("status").Scan(&rows).Error; err != nil {
		return view, ErrStorage
	}
	for _, row := range rows {
		view.Total += row.Count
		switch row.Status {
		case "succeeded":
			view.Completed += row.Count
		case "manual_review":
			view.NeedsAttention += row.Count
		default:
			view.Pending += row.Count
		}
	}
	return view, nil
}

func (s *Service) SaveTranslationSettings(ctx context.Context, p Principal, in TranslationSettingsInput) (TranslationStatus, error) {
	if !p.IsAdmin() {
		return TranslationStatus{}, ErrForbidden
	}
	in.SourceLocale = canonicalLocale(in.SourceLocale)
	if (in.Edition != "free" && in.Edition != "pro") || in.SourceLocale == "" {
		return TranslationStatus{}, ErrInvalid
	}
	var current storage.TranslationSettings
	findErr := s.db.WithContext(ctx).First(&current, "id = ?", 1).Error
	if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return TranslationStatus{}, ErrStorage
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" && findErr == nil {
		plain, err := s.readSecret(ctx, current.CredentialSecretID)
		if err != nil {
			return TranslationStatus{}, err
		}
		key = string(plain)
	}
	if key == "" || len(key) > 512 || strings.ContainsAny(key, "\r\n") {
		return TranslationStatus{}, problem(400, "DEEPL_API_KEY_REQUIRED", "首次连接需要填写 DeepL API Key")
	}
	client, err := s.translationProvider(in.Edition, key)
	if err != nil {
		return TranslationStatus{}, problem(400, "DEEPL_CONFIG_INVALID", "DeepL 配置格式无效")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	usage, err := client.Usage(checkCtx)
	cancel()
	if err != nil {
		return TranslationStatus{}, problem(400, "DEEPL_CONNECTION_FAILED", "无法连接 DeepL，请检查版本和 API Key")
	}
	checked := now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		secretID := current.CredentialSecretID
		if in.APIKey != "" {
			var secretErr error
			secretID, secretErr = s.createSecret(tx, "deepl_api_key", []byte(key))
			if secretErr != nil {
				return secretErr
			}
		}
		settings := storage.TranslationSettings{ID: 1, Provider: "deepl", Edition: in.Edition, SourceLocale: in.SourceLocale, CredentialSecretID: secretID, CharacterCount: usage.CharacterCount, CharacterLimit: usage.CharacterLimit, LastCheckedAt: &checked}
		if findErr == nil {
			if err := tx.Model(&storage.TranslationSettings{}).Where("id = ?", 1).Updates(map[string]any{"edition": settings.Edition, "source_locale": settings.SourceLocale, "credential_secret_id": settings.CredentialSecretID, "character_count": settings.CharacterCount, "character_limit": settings.CharacterLimit, "last_checked_at": settings.LastCheckedAt, "last_check_error": nil}).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&settings).Error; err != nil {
			return err
		}
		force := findErr == nil && current.SourceLocale != in.SourceLocale
		if err := s.enqueueAllTranslations(tx, in.SourceLocale, force); err != nil {
			return err
		}
		return audit(tx, p.User.ID, "translation.configure", "translation_settings", "1", "")
	})
	if err != nil {
		return TranslationStatus{}, databaseError(err)
	}
	return s.TranslationStatus(ctx, p)
}

func (s *Service) SyncTranslations(ctx context.Context, p Principal) (TranslationStatus, error) {
	if !p.IsAdmin() {
		return TranslationStatus{}, ErrForbidden
	}
	var settings storage.TranslationSettings
	if err := s.db.WithContext(ctx).First(&settings, "id = ?", 1).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return TranslationStatus{}, problem(409, "DEEPL_NOT_CONFIGURED", "请先连接 DeepL")
	} else if err != nil {
		return TranslationStatus{}, ErrStorage
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.enqueueAllTranslations(tx, settings.SourceLocale, true); err != nil {
			return err
		}
		return audit(tx, p.User.ID, "translation.sync", "translation_settings", "1", "")
	}); err != nil {
		return TranslationStatus{}, databaseError(err)
	}
	return s.TranslationStatus(ctx, p)
}

func (s *Service) claimTranslation(ctx context.Context) (storage.TranslationTask, string, bool, error) {
	lease, err := newID()
	if err != nil {
		return storage.TranslationTask{}, "", false, err
	}
	var task storage.TranslationTask
	at := now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("((status IN ('pending','retrying') AND available_at <= ?) OR (status = 'running' AND lease_until < ?))", at, at).Order("available_at,created_at").First(&task)
		if query.Error != nil {
			return query.Error
		}
		until := at.Add(3 * time.Minute)
		if err := tx.Model(&task).Updates(map[string]any{"status": "running", "attempts": gorm.Expr("attempts + 1"), "lease_token": lease, "lease_until": until, "last_error_code": nil}).Error; err != nil {
			return err
		}
		task.Attempts++
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return storage.TranslationTask{}, "", false, nil
	}
	if err != nil {
		return storage.TranslationTask{}, "", false, databaseError(err)
	}
	return task, lease, true, nil
}

func translatePayload(ctx context.Context, client ports.Translator, sourceLocale, targetLocale string, raw json.RawMessage) (json.RawMessage, error) {
	var payload TranslationPayload
	if json.Unmarshal(raw, &payload) != nil {
		return nil, ErrStorage
	}
	texts := make([]string, 0, 18+len(payload.Features)+len(payload.PlatformRequirements))
	setters := make([]func(string), 0, cap(texts))
	finalizers := make([]func(), 0, 4)
	add := func(value *string) {
		if strings.TrimSpace(*value) == "" {
			return
		}
		texts = append(texts, *value)
		setters = append(setters, func(translated string) { *value = translated })
	}
	type documentPart struct {
		text  string
		image bool
	}
	addDocument := func(value *string) {
		if strings.TrimSpace(*value) == "" {
			return
		}
		lines := strings.Split(strings.ReplaceAll(*value, "\r\n", "\n"), "\n")
		parts := make([]documentPart, 0, 3)
		plain := make([]string, 0, len(lines))
		flush := func() {
			if len(plain) > 0 {
				parts = append(parts, documentPart{text: strings.Join(plain, "\n")})
				plain = nil
			}
		}
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "![") && strings.Contains(trimmed, "](/api/v1/product-document-images/") && strings.HasSuffix(trimmed, ")") {
				flush()
				parts = append(parts, documentPart{text: trimmed, image: true})
				continue
			}
			plain = append(plain, line)
		}
		flush()
		if len(parts) == 1 && !parts[0].image {
			add(value)
			return
		}
		for index := range parts {
			if parts[index].image || strings.TrimSpace(parts[index].text) == "" {
				continue
			}
			texts = append(texts, parts[index].text)
			index := index
			setters = append(setters, func(translated string) { parts[index].text = translated })
		}
		finalizers = append(finalizers, func() {
			values := make([]string, len(parts))
			for index := range parts {
				values[index] = parts[index].text
			}
			*value = strings.Join(values, "\n")
		})
	}
	add(&payload.Name)
	add(&payload.Tagline)
	add(&payload.Description)
	add(&payload.Summary)
	for i := range payload.Features {
		add(&payload.Features[i])
	}
	add(&payload.Requirements)
	platformKeys := make([]string, 0, len(payload.PlatformRequirements))
	for key := range payload.PlatformRequirements {
		platformKeys = append(platformKeys, key)
	}
	sort.Strings(platformKeys)
	for _, key := range platformKeys {
		value := payload.PlatformRequirements[key]
		if strings.TrimSpace(value) == "" {
			continue
		}
		texts = append(texts, value)
		key := key
		setters = append(setters, func(translated string) { payload.PlatformRequirements[key] = translated })
	}
	addDocument(&payload.InstallationGuide)
	addDocument(&payload.UpgradeGuide)
	addDocument(&payload.UninstallGuide)
	addDocument(&payload.TroubleshootingGuide)
	add(&payload.TermsOfService)
	add(&payload.PrivacyPolicy)
	add(&payload.RefundPolicy)
	add(&payload.InvoiceGuide)
	add(&payload.LegalTaxNotice)
	add(&payload.OfflineSummary)
	for offset := 0; offset < len(texts); offset += 50 {
		end := min(offset+50, len(texts))
		translated, err := client.Translate(ctx, deeplLanguage(sourceLocale, false), deeplLanguage(targetLocale, true), texts[offset:end])
		if err != nil {
			return nil, err
		}
		if len(translated) != end-offset {
			return nil, deepl.ErrRequest
		}
		for i := range translated {
			setters[offset+i](translated[i])
		}
	}
	for _, finalize := range finalizers {
		finalize()
	}
	return marshal(payload), nil
}

func (s *Service) failTranslation(ctx context.Context, task storage.TranslationTask, lease string) error {
	status := "retrying"
	if task.Attempts >= 8 {
		status = "manual_review"
	}
	code := "DEEPL_REQUEST_FAILED"
	delay := time.Duration(1<<min(task.Attempts, 10)) * time.Minute
	return databaseError(s.db.WithContext(ctx).Model(&storage.TranslationTask{}).Where("id = ? AND lease_token = ?", task.ID, lease).Updates(map[string]any{"status": status, "available_at": now().Add(delay), "lease_token": nil, "lease_until": nil, "last_error_code": code}).Error)
}

func (s *Service) ProcessTranslation(ctx context.Context) (bool, error) {
	var settings storage.TranslationSettings
	if err := s.db.WithContext(ctx).First(&settings, "id = ?", 1).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, ErrStorage
	}
	task, lease, handled, err := s.claimTranslation(ctx)
	if err != nil || !handled {
		return handled, err
	}
	key, err := s.readSecret(ctx, settings.CredentialSecretID)
	if err != nil {
		return true, s.failTranslation(context.WithoutCancel(ctx), task, lease)
	}
	client, err := s.translationProvider(settings.Edition, string(key))
	clear(key)
	if err != nil {
		return true, s.failTranslation(context.WithoutCancel(ctx), task, lease)
	}
	workCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for _, locale := range localeOptions {
		if locale.Code == task.SourceLocale {
			continue
		}
		var existing storage.ContentTranslation
		err := s.db.WithContext(workCtx).First(&existing, "resource_type = ? AND resource_id = ? AND locale = ? AND source_hash = ?", task.ResourceType, task.ResourceID, locale.Code, task.SourceHash).Error
		if err == nil {
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return true, s.failTranslation(context.WithoutCancel(ctx), task, lease)
		}
		payload, err := translatePayload(workCtx, client, task.SourceLocale, locale.Code, task.SourceJSON)
		if err != nil {
			return true, s.failTranslation(context.WithoutCancel(ctx), task, lease)
		}
		row := storage.ContentTranslation{ResourceType: task.ResourceType, ResourceID: task.ResourceID, Locale: locale.Code, SourceHash: append([]byte(nil), task.SourceHash...), Payload: payload}
		if err := s.db.WithContext(workCtx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
			return true, s.failTranslation(context.WithoutCancel(ctx), task, lease)
		}
	}
	finish := context.WithoutCancel(ctx)
	if err := s.db.WithContext(finish).Model(&storage.TranslationTask{}).Where("id = ? AND lease_token = ?", task.ID, lease).Updates(map[string]any{"status": "succeeded", "lease_token": nil, "lease_until": nil, "last_error_code": nil}).Error; err != nil {
		return true, databaseError(err)
	}
	if usage, usageErr := client.Usage(finish); usageErr == nil {
		checked := now()
		_ = s.db.WithContext(finish).Model(&storage.TranslationSettings{}).Where("id = ?", 1).Updates(map[string]any{"character_count": usage.CharacterCount, "character_limit": usage.CharacterLimit, "last_checked_at": checked}).Error
	}
	return true, nil
}

func (s *Service) localizedPayload(ctx context.Context, resourceType, resourceID, locale string, source TranslationPayload) (TranslationPayload, bool) {
	if locale == "" {
		return source, false
	}
	raw := marshal(source)
	hash := sha256.Sum256(raw)
	var row storage.ContentTranslation
	if s.db.WithContext(ctx).First(&row, "resource_type = ? AND resource_id = ? AND locale = ? AND source_hash = ?", resourceType, resourceID, locale, hash[:]).Error != nil {
		return source, false
	}
	var translated TranslationPayload
	if json.Unmarshal(row.Payload, &translated) != nil {
		return source, false
	}
	return translated, true
}
