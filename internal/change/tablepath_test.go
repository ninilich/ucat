package change_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ninilich/ucat/internal/change"
)

func TestTablePathLabel(t *testing.T) {
	assert.Equal(t, "silver.orders", change.TablePath{Catalog: "dev", Schema: litSilver, Name: litOrders}.Label())
}

// Subject is the one table reference of differ and preflight diagnostics.
func TestTablePathSubject(t *testing.T) {
	p := change.TablePath{Catalog: "dev", Schema: litSilver, Name: litOrders}
	assert.Equal(t, `table "silver.orders"`, p.Subject())
	// Exotic names stay readable and unambiguous (quoted with Go escaping).
	assert.Equal(t, `table "s.a\"b"`, change.TablePath{Schema: "s", Name: `a"b`}.Subject())
}
