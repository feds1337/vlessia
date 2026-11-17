package core

import (
	"errors"
	"strings"
	"unicode"

	"github.com/feds1337/vlessia/internal/xray"
)

// ValidateRule validates a single entry without changing any settings.
func ValidateRule(kind, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", errors.New("правило должно содержать одно непустое значение")
	}
	switch kind {
	case "site":
		domains, ips, err := xray.ParseRules([]string{value})
		if err != nil {
			return "", err
		}
		if len(domains)+len(ips) != 1 {
			return "", errors.New("пустое правило")
		}
	case "app":
		if strings.ContainsAny(value, "*?\"") {
			return "", errors.New("укажите имя приложения или путь без маски")
		}
	default:
		return "", errors.New("неизвестный тип правила")
	}
	return value, nil
}
