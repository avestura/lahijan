package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidatePromoCode(t *testing.T) {
	t.Parallel()
	assert.NoError(t, validatePromoCode("SPRING-2026"))
	for _, bad := range []string{"", "lower", "HAS SPACE", "EMOJI-😀", "under_score"} {
		assert.ErrorIs(t, validatePromoCode(bad), ErrInvalidPromoCode, bad)
	}
}

func TestCurrencyOrDefault(t *testing.T) {
	t.Parallel()
	s := &Service{config: Config{Currency: "USD"}}
	assert.Equal(t, "USD", s.currencyOrDefault(""))
	assert.Equal(t, "EUR", s.currencyOrDefault("EUR"))
}
