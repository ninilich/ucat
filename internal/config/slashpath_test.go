package config_test

import (
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
)

// SlashPath must not depend on filepath.Separator: the same input gives the same text on any OS.
func TestSlashPath_Table(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		litEmpty:        {"", ""},
		"backslash":     {`a\b`, pathAB},
		"slash":         {"a/b", "a/b"},
		"mixed":         {`a\b/c`, "a/b/c"},
		"drive":         {`C:\x\y`, "C:/x/y"},
		"unc":           {`\\server\share\x`, "//server/share/x"},
		"already clean": {pathSilverOrders, pathSilverOrders},
		"single name":   {"orders.yml", "orders.yml"},
		"trailing":      {`tables\prod\`, "tables/prod/"},
		"only slashes":  {`\\`, "//"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, config.SlashPath(tc.in))
			assert.Equal(t, tc.want, config.SlashPath(config.SlashPath(tc.in)), "idempotent")
		})
	}
}
