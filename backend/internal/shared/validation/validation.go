package validation

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

type Errors map[string]string

func (e Errors) Add(field, message string) {
	if _, exists := e[field]; !exists {
		e[field] = message
	}
}

func (e Errors) Any() bool {
	return len(e) > 0
}

func Required(value, message string, errs Errors, field string) {
	if strings.TrimSpace(value) == "" {
		errs.Add(field, message)
	}
}

func MaxRunes(value string, limit int, message string, errs Errors, field string) {
	if utf8.RuneCountInString(strings.TrimSpace(value)) > limit {
		errs.Add(field, message)
	}
}

func ValidEmail(value, message string, errs Errors, field string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	if _, err := mail.ParseAddress(value); err != nil {
		errs.Add(field, message)
	}
}
