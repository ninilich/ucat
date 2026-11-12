package config_test

import (
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
)

// The layout is a contract with existing projects: these literals must not change.
func TestLayoutConstants(t *testing.T) {
	assert.Equal(t, "ucat.yml", config.ProjectFile)
	assert.Equal(t, "catalogs", config.CatalogsDir)
	assert.Equal(t, "targets", config.TargetsDir)
	assert.Equal(t, "tables", config.DefaultTablesDir)
	assert.Equal(t, ".yml", config.YAMLExt)
}

func TestLayoutFiles(t *testing.T) {
	assert.Equal(t, "catalogs/main.yml", config.CatalogFile("main"))
	assert.Equal(t, "targets/dev.yml", config.TargetFile("dev"))
	assert.Equal(t, "targets/<name>.yml", config.TargetFile("<name>"))
	assert.Equal(t, "catalogs/.yml", config.CatalogFile(""), "no validation here: callers validate names")
}

func TestTableFile(t *testing.T) {
	cases := map[string]struct{ dir, schema, name, want string }{
		"default":         {"tables", litSilver2, litOrders, "tables/silver/orders.yml"},
		"nested dir":      {"defs/tables", litSilver2, litOrders, "defs/tables/silver/orders.yml"},
		"backslash dir":   {`defs\tables`, litSilver2, litOrders, "defs/tables/silver/orders.yml"},
		"trailing slash":  {"tables/", litSilver2, litOrders, "tables/silver/orders.yml"},
		"dot dir":         {".", litSilver2, "orders", "silver/orders.yml"},
		"parent dir":      {"../x", litSilver2, "orders", "../x/silver/orders.yml"},
		"unicode":         {"таблицы", "серебро", "заказы", "таблицы/серебро/заказы.yml"},
		"windows trailer": {`tables\`, "s", "t", "tables/s/t.yml"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.want, config.TableFile(c.dir, c.schema, c.name))
		})
	}
}
