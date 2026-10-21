package app

import (
	"encoding/json"
	"math"
	"sort"

	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/storage"
)

var supportedPurchasePeriods = [...]uint64{1, 3, 6, 12}

func defaultPurchaseOptions(amount int64) []PurchaseOption {
	options := make([]PurchaseOption, 0, len(supportedPurchasePeriods))
	for _, periods := range supportedPurchasePeriods {
		value := amount
		if amount > 0 && periods <= uint64(math.MaxInt64/amount) {
			value = amount * int64(periods)
		}
		options = append(options, PurchaseOption{Periods: periods, AmountMinor: value, Enabled: true})
	}
	return options
}

func purchaseOptions(version storage.PlanVersion) []PurchaseOption {
	if version.PurchaseType != "fixed_term" {
		return []PurchaseOption{}
	}
	var options []PurchaseOption
	if len(version.PurchaseOptions) > 0 && json.Unmarshal(version.PurchaseOptions, &options) == nil && len(options) > 0 {
		return options
	}
	return defaultPurchaseOptions(version.AmountMinor)
}

func normalizePurchaseOptions(options []PurchaseOption, fallback int64, currency string) ([]PurchaseOption, error) {
	if len(options) == 0 {
		options = defaultPurchaseOptions(fallback)
	}
	allowed := map[uint64]bool{1: true, 3: true, 6: true, 12: true}
	byPeriod := make(map[uint64]PurchaseOption, len(options))
	for _, option := range options {
		if !allowed[option.Periods] || byPeriod[option.Periods].Periods != 0 || (domain.Money{Currency: currency, Minor: option.AmountMinor}).Validate() != nil || option.Enabled && option.AmountMinor <= 0 {
			return nil, ErrInvalid
		}
		byPeriod[option.Periods] = option
	}
	result := make([]PurchaseOption, 0, len(supportedPurchasePeriods))
	enabled := 0
	for _, periods := range supportedPurchasePeriods {
		option, ok := byPeriod[periods]
		if !ok {
			return nil, ErrInvalid
		}
		if option.Enabled {
			enabled++
		}
		result = append(result, option)
	}
	if enabled == 0 {
		return nil, problem(400, "PURCHASE_OPTION_REQUIRED", "请至少启用一个购买期限")
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Periods < result[j].Periods })
	return result, nil
}

func purchaseAmount(version storage.PlanVersion, periods uint64) (int64, error) {
	if periods == 0 {
		periods = 1
	}
	if version.PurchaseType == "perpetual" {
		if periods != 1 {
			return 0, problem(400, "PURCHASE_PERIOD_INVALID", "永久套餐不需要选择购买期限")
		}
		return version.AmountMinor, nil
	}
	for _, option := range purchaseOptions(version) {
		if option.Periods == periods && option.Enabled {
			return option.AmountMinor, nil
		}
	}
	return 0, problem(400, "PURCHASE_PERIOD_INVALID", "请选择当前套餐已启用的购买期限")
}
