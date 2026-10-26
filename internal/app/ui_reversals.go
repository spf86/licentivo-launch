package app

// Transaction status copy is bundled; it never consumes translation quota.
var reversalUI = map[string][4]string{
	"zh-CN": {"购买已终止", "已撤款", "订单已撤款", "支付渠道已确认撤款，该订单对应的购买权益已移除。如有疑问，请联系商家核对。"},
	"en":    {"Purchase ended", "Payment reversed", "Order payment reversed", "The payment provider confirmed a reversal. This order's purchased coverage has been removed. Contact the merchant if you need help."},
	"ja":    {"購入期間終了", "支払い取消済み", "注文の支払いが取り消されました", "決済サービスが支払いの取り消しを確認したため、この注文の購入分は無効になりました。ご不明な点は販売者にお問い合わせください。"},
	"ko":    {"구매 종료", "결제 취소됨", "주문 결제가 취소되었습니다", "결제 서비스가 취소를 확인하여 이 주문의 구매 내역이 더 이상 적용되지 않습니다. 문의 사항은 판매자에게 연락하세요."},
	"de":    {"Kauf beendet", "Zahlung zurückgebucht", "Bestellzahlung zurückgebucht", "Der Zahlungsanbieter hat die Rückbuchung bestätigt. Die mit dieser Bestellung erworbene Laufzeit wurde entfernt. Bei Fragen wenden Sie sich an den Händler."},
	"fr":    {"Achat terminé", "Paiement annulé", "Paiement de la commande annulé", "Le prestataire de paiement a confirmé l’annulation. Les droits achetés avec cette commande ont été retirés. Contactez le vendeur pour toute question."},
	"es":    {"Compra finalizada", "Pago revertido", "Pago del pedido revertido", "El proveedor de pagos confirmó la reversión. Se retiró la cobertura adquirida con este pedido. Contacta con la tienda si necesitas ayuda."},
	"pt-BR": {"Compra encerrada", "Pagamento revertido", "Pagamento do pedido revertido", "O provedor de pagamentos confirmou a reversão. A cobertura adquirida neste pedido foi removida. Entre em contato com a loja se precisar de ajuda."},
	"it":    {"Acquisto terminato", "Pagamento annullato", "Pagamento dell’ordine annullato", "Il gestore dei pagamenti ha confermato l’annullamento. La copertura acquistata con questo ordine è stata rimossa. Per assistenza, contatta il venditore."},
	"ru":    {"Покупка прекращена", "Платёж отменён", "Платёж по заказу отменён", "Платёжный сервис подтвердил отмену платежа. Оплаченный этим заказом период больше не действует. По вопросам обратитесь к продавцу."},
}

func reversalStrings(locale string) map[string]string {
	values, ok := reversalUI[canonicalLocale(locale)]
	if !ok {
		values = reversalUI["en"]
	}
	return map[string]string{"account.revoked": values[0], "account.order_reversed": values[1], "order.reversed": values[2], "order.reversed_body": values[3]}
}
