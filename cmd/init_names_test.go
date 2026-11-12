package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type initCatalogFile struct {
	Name    string `yaml:"name"`
	UCName  string `yaml:"uc_name"`
	Schemas map[string]struct {
		UCName string `yaml:"uc_name"`
	} `yaml:"schemas"`
}

func validInitName(s string) bool {
	return s != "" && !strings.ContainsAny(s, ". \t")
}

func validInitUCName(s string) bool {
	return validInitName(s) && s == strings.ToLower(s)
}

// assertInitCatalogsValid checks every generated catalog file holds only valid names.
func assertInitCatalogsValid(t *testing.T, dir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "catalogs", "*.yml"))
	require.NoError(t, err)
	for _, f := range files {
		data, err := os.ReadFile(f)
		require.NoError(t, err)
		var c initCatalogFile
		require.NoError(t, yaml.Unmarshal(data, &c), "%s must be valid YAML", f)
		assert.True(t, validInitName(c.Name), "logical catalog %q", c.Name)
		assert.True(t, validInitUCName(c.UCName), "uc catalog %q", c.UCName)
		assert.True(t, validInitName(strings.TrimSuffix(filepath.Base(f), ".yml")), "file %s", f)
		for s, sc := range c.Schemas {
			assert.True(t, validInitName(s), "logical schema %q", s)
			assert.True(t, validInitUCName(sc.UCName), "uc schema %q", sc.UCName)
		}
	}
}

// Invalid names must be rejected (error) or re-prompted; never written to files as is.
func TestInit_RejectsInvalidNames(t *testing.T) {
	good := strings.Repeat("good\n", 12)
	cases := map[string]string{
		"catalog with dot":     "proj\na.b\n" + good,
		"catalog with space":   "proj\na b\n" + good,
		"uc catalog uppercase": "proj\nmain\nsilver\nsilver\ndev\nh\nMAIN\n" + good,
		"uc catalog with dot":  "proj\nmain\nsilver\nsilver\ndev\nh\nx.y\n" + good,
		"schema with dot":      "proj\nmain\nx.y\n" + good,
		"schema with space":    "proj\nmain\nx y\n" + good,
		"uc schema uppercase":  "proj\nmain\nsilver\nSilver\n" + good,
		"uc schema with dot":   "proj\nmain\nsilver\ns.v\n" + good,
	}
	for name, stdin := range cases {
		t.Run(name, func(t *testing.T) {
			emptyDir(t)
			dir, err := os.Getwd()
			require.NoError(t, err)
			if _, err = executeInitCmd(t, stdin); err != nil {
				return
			}
			assertInitCatalogsValid(t, dir)
		})
	}
}

// Values that are special in YAML must survive a round trip through the generated files.
func TestInit_EscapesYAMLValues(t *testing.T) {
	emptyDir(t)
	dir, err := os.Getwd()
	require.NoError(t, err)

	project := "my: proj #x"
	host := "https://h.example.com #frag"
	warehouse := "a: b"
	ucCatalog := "a:b#c"
	stdin := project + "\nmain\nsilver\nsilver\ndev\n" + host + "\n" + ucCatalog + "\n" + warehouse + "\n"
	out, err := executeInitCmd(t, stdin)
	require.NoError(t, err, out)

	var ucat map[string]any
	data, err := os.ReadFile(filepath.Join(dir, "ucat.yml"))
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &ucat), "ucat.yml:\n%s", data)
	assert.Equal(t, project, ucat["project"])
	assert.Equal(t, "tables", ucat["tables_dir"])

	var tgt struct {
		Workspace map[string]any `yaml:"workspace"`
		Catalogs  map[string]struct {
			UCName string `yaml:"uc_name"`
		} `yaml:"catalogs"`
	}
	data, err = os.ReadFile(filepath.Join(dir, "targets", "dev.yml"))
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &tgt), "dev.yml:\n%s", data)
	assert.Equal(t, host, tgt.Workspace["host"])
	assert.Equal(t, warehouse, tgt.Workspace["warehouse_id"])
	assert.Len(t, tgt.Workspace, 2)
	assert.Equal(t, ucCatalog, tgt.Catalogs["main"].UCName)
}

// Scalars YAML would retype (bool, number) must stay strings after parsing back.
func TestInit_EscapesYAMLTypedScalars(t *testing.T) {
	emptyDir(t)
	dir, err := os.Getwd()
	require.NoError(t, err)

	out, err := executeInitCmd(t, "true\nmain\nsilver\nsilver\ndev\nhttps://h.example.com\n123\n123456\n")
	require.NoError(t, err, out)

	var ucat map[string]any
	data, err := os.ReadFile(filepath.Join(dir, "ucat.yml"))
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &ucat))
	assert.Equal(t, "true", ucat["project"])

	var tgt struct {
		Workspace map[string]any `yaml:"workspace"`
		Catalogs  map[string]map[string]any
	}
	data, err = os.ReadFile(filepath.Join(dir, "targets", "dev.yml"))
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, &tgt))
	assert.Equal(t, "123456", tgt.Workspace["warehouse_id"])
	assert.Equal(t, "123", tgt.Catalogs["main"]["uc_name"], "a numeric UC catalog name stays a string")
}
