package app

// Registration and recovery copy is bundled even when no email provider exists.
var registrationUI = map[string][]string{
	"zh-CN": {"使用邮箱和密码创建账号，注册后即可登录。", "本站未启用邮件，请联系商家找回密码。", "密码至少 12 个字符。", "注册已完成，请使用邮箱和密码登录", "无需邮箱验证", "如需修改邮箱，请联系商家。"},
	"en":    {"Create an account with your email and password. You can sign in immediately.", "Email is disabled. Contact the store to recover your account.", "Use at least 12 characters for your password.", "Registration complete. Sign in with your email and password.", "Email verification is optional", "Contact the store to change your email address."},
	"ja":    {"メールアドレスとパスワードで登録し、すぐにログインできます。", "メール機能は無効です。アカウントの復旧は販売者にお問い合わせください。", "パスワードは12文字以上にしてください。", "登録が完了しました。メールアドレスとパスワードでログインしてください。", "メール認証は不要です", "メールアドレスの変更は販売者にお問い合わせください。"},
	"ko":    {"이메일과 비밀번호로 가입하면 바로 로그인할 수 있습니다.", "이메일 기능이 꺼져 있습니다. 계정 복구는 판매자에게 문의하세요.", "비밀번호는 12자 이상이어야 합니다.", "가입이 완료되었습니다. 이메일과 비밀번호로 로그인하세요.", "이메일 인증이 필요하지 않습니다", "이메일 변경은 판매자에게 문의하세요."},
	"de":    {"Registrieren Sie sich mit E-Mail und Passwort und melden Sie sich sofort an.", "E-Mail ist deaktiviert. Wenden Sie sich zur Kontowiederherstellung an den Händler.", "Verwenden Sie mindestens 12 Zeichen für Ihr Passwort.", "Registrierung abgeschlossen. Melden Sie sich mit E-Mail und Passwort an.", "E-Mail-Bestätigung ist optional", "Wenden Sie sich zum Ändern Ihrer E-Mail-Adresse an den Händler."},
	"fr":    {"Créez un compte avec votre e-mail et votre mot de passe, puis connectez-vous immédiatement.", "Les e-mails sont désactivés. Contactez le vendeur pour récupérer votre compte.", "Utilisez au moins 12 caractères pour votre mot de passe.", "Inscription terminée. Connectez-vous avec votre e-mail et votre mot de passe.", "La vérification de l’e-mail est facultative", "Contactez le vendeur pour modifier votre adresse e-mail."},
	"es":    {"Crea una cuenta con tu correo y contraseña e inicia sesión de inmediato.", "El correo está desactivado. Contacta con la tienda para recuperar tu cuenta.", "Usa al menos 12 caracteres para la contraseña.", "Registro completado. Inicia sesión con tu correo y contraseña.", "La verificación del correo es opcional", "Contacta con la tienda para cambiar tu correo."},
	"pt-BR": {"Crie uma conta com e-mail e senha e entre imediatamente.", "O e-mail está desativado. Contate a loja para recuperar sua conta.", "Use pelo menos 12 caracteres na senha.", "Cadastro concluído. Entre com seu e-mail e senha.", "A verificação de e-mail é opcional", "Contate a loja para alterar seu e-mail."},
	"it":    {"Crea un account con e-mail e password e accedi subito.", "L’e-mail è disattivata. Contatta il negozio per recuperare il tuo account.", "Usa almeno 12 caratteri per la password.", "Registrazione completata. Accedi con e-mail e password.", "La verifica e-mail è facoltativa", "Contatta il negozio per modificare il tuo indirizzo e-mail."},
	"ru":    {"Создайте аккаунт с адресом электронной почты и паролем и сразу войдите.", "Почта отключена. Для восстановления аккаунта обратитесь к продавцу.", "Пароль должен содержать не менее 12 символов.", "Регистрация завершена. Войдите с адресом почты и паролем.", "Подтверждение почты необязательно", "Для изменения адреса почты обратитесь к продавцу."},
}

func registrationStrings(locale string) map[string]string {
	keys := []string{"auth.register_direct", "auth.mail_unavailable", "auth.password_help", "toast.registration_ready", "account.email_optional", "account.email_change_unavailable"}
	texts, ok := registrationUI[canonicalLocale(locale)]
	if !ok {
		texts = registrationUI["en"]
	}
	out := make(map[string]string, len(keys))
	for i, key := range keys {
		out[key] = texts[i]
	}
	return out
}
