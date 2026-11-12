package pull_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/pull"
)

func TestPulledType(t *testing.T) {
	for in, want := range map[string]string{
		"VARCHAR(10)": typSTRING, " char ( 3 ) ": typSTRING, "varchar(1)": typSTRING,
		"STRING": "STRING", "ARRAY<VARCHAR(5)>": "ARRAY<VARCHAR(5)>", "DECIMAL(10,2)": "DECIMAL(10,2)", "VARCHAR": "VARCHAR",
	} {
		assert.Equal(t, want, pull.PulledType(in), in)
	}
}

func TestRenderDoesNotMutateInput(t *testing.T) {
	in := backendtest.Live("prod", "s", "t", backendtest.Col("a", "VARCHAR(5)"), backendtest.Col("b", "INT"))
	f, _ := pull.ParseFilter("")
	data, diags, err := pull.Render(in, "silver", "tables/s/t.yml", f)
	require.NoError(t, err)
	assert.Contains(t, string(data), "type: STRING")
	assert.NotContains(t, string(data), "VARCHAR")
	require.Len(t, diags, 1)
	assert.Equal(t, "tables/s/t.yml", diags[0].Location)
	assert.Contains(t, diags[0].Summary, `column "a": VARCHAR(5) pulled as STRING`)
	assert.Equal(t, "VARCHAR(5)", in.Columns[0].Type)
}

func TestRenderFiltersTblPropertiesAndNestedClustering(t *testing.T) {
	in := backendtest.Live("prod", "s", "t", backendtest.Col("a", "INT"))
	in.Properties = map[string]string{"keep": "1", "drop": "1"}
	in.Clustering = []string{litAB}
	f, _ := pull.ParseFilter("keep")
	data, diags, err := pull.Render(in, "silver", "x.yml", f)
	require.NoError(t, err)
	assert.Contains(t, string(data), "keep")
	assert.NotContains(t, string(data), "drop")
	assert.NotContains(t, string(data), "clustering")
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Summary, "nested field")
	assert.Equal(t, []string{"a.b"}, in.Clustering, "input keeps its clustering")
	assert.Len(t, in.Properties, 2)
}

// The pulled file has no nullable key for a nullable column (true is the default) and nullable: false
// for a NOT NULL one; an empty comment is not written.
func TestRenderNullableAndComment(t *testing.T) {
	in := backendtest.Live("prod", "s", "t", backendtest.Col("a", "INT"), backendtest.NotNull(backendtest.Col("b", "INT")))
	f, _ := pull.ParseFilter("")
	data, diags, err := pull.Render(in, "silver", "x.yml", f)
	require.NoError(t, err)
	assert.Empty(t, diags)
	assert.Equal(t, `name: t
schema: silver
columns:
    - name: a
      type: INT
    - name: b
      type: INT
      nullable: false
`, string(data))
}
