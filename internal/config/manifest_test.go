package config_test

import (
	"testing"

	"github.com/ninilich/ucat/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// tblproperties deserialization contract.
// If TblProperties is changed to map[string]*string, YAML null must
// deserialize to a nil pointer (UNSET semantics). These tests document that contract
// using a local struct and verify gopkg.in/yaml.v3 behavior.

// tableWithPtrProps mirrors the target type for TblProperties.
type tableWithPtrProps struct {
	Name          string             `yaml:"name"`
	TblProperties map[string]*string `yaml:"tblproperties,omitempty"`
}

// TestManifest_TblProperties_NullDeserializesToNilPtr verifies that
// `tblproperties: {key: null}` in YAML deserializes to map[string]*string
// where the value for "key" is a nil pointer.
// This documents the expected behavior after the change is applied.
func TestManifest_TblProperties_NullDeserializesToNilPtr(t *testing.T) {
	yamlInput := `
name: orders
tblproperties:
  myKey: null
`
	var tbl tableWithPtrProps
	require.NoError(t, yaml.Unmarshal([]byte(yamlInput), &tbl))

	require.Contains(t, tbl.TblProperties, "myKey",
		"'myKey' must be present in the deserialized map")
	assert.Nil(t, tbl.TblProperties["myKey"],
		"null in YAML must deserialize to nil *string (UNSET semantics)")
}

// TestManifest_TblProperties_StringValueDeserializesToNonNilPtr verifies that
// a non-null value in YAML deserializes to a non-nil *string.
func TestManifest_TblProperties_StringValueDeserializesToNonNilPtr(t *testing.T) {
	yamlInput := `
name: orders
tblproperties:
  delta.columnMapping.mode: name
`
	var tbl tableWithPtrProps
	require.NoError(t, yaml.Unmarshal([]byte(yamlInput), &tbl))

	require.Contains(t, tbl.TblProperties, "delta.columnMapping.mode")
	require.NotNil(t, tbl.TblProperties["delta.columnMapping.mode"])
	assert.Equal(t, "name", *tbl.TblProperties["delta.columnMapping.mode"])
}

// TestManifest_TblProperties_MixedNullAndValue verifies the mixed case:
// some keys are null (UNSET), some are non-null (SET).
func TestManifest_TblProperties_MixedNullAndValue(t *testing.T) {
	yamlInput := `
name: orders
tblproperties:
  setKey: someValue
  unsetKey: null
`
	var tbl tableWithPtrProps
	require.NoError(t, yaml.Unmarshal([]byte(yamlInput), &tbl))

	require.Contains(t, tbl.TblProperties, "setKey")
	require.NotNil(t, tbl.TblProperties["setKey"],
		"non-null value must deserialize to non-nil pointer")
	assert.Equal(t, "someValue", *tbl.TblProperties["setKey"])

	require.Contains(t, tbl.TblProperties, "unsetKey")
	assert.Nil(t, tbl.TblProperties["unsetKey"],
		"null value must deserialize to nil pointer (UNSET semantics)")
}

// TestManifest_TblProperties_OmitEmptyWhenNil verifies that an empty/nil TblProperties
// map is omitted from YAML output (omitempty behavior).
func TestManifest_TblProperties_OmitEmptyWhenNil(t *testing.T) {
	tbl := tableWithPtrProps{Name: litOrders}
	// TblProperties is nil (not set)

	data, err := yaml.Marshal(&tbl)
	require.NoError(t, err)

	yamlStr := string(data)
	assert.NotContains(t, yamlStr, "tblproperties",
		"nil TblProperties must be omitted from YAML with omitempty")
}

// TestManifest_TblProperties_NonNilPtrSerializesAsString verifies that a non-nil
// *string in TblProperties round-trips correctly through YAML marshal/unmarshal.
func TestManifest_TblProperties_NonNilPtrSerializesAsString(t *testing.T) {
	val := "enabled"
	tbl := tableWithPtrProps{
		Name: litOrders,
		TblProperties: map[string]*string{
			"delta.enableChangeDataFeed": &val,
		},
	}

	data, err := yaml.Marshal(&tbl)
	require.NoError(t, err)

	var roundTripped tableWithPtrProps
	require.NoError(t, yaml.Unmarshal(data, &roundTripped))

	require.Contains(t, roundTripped.TblProperties, "delta.enableChangeDataFeed")
	require.NotNil(t, roundTripped.TblProperties["delta.enableChangeDataFeed"])
	assert.Equal(t, "enabled", *roundTripped.TblProperties["delta.enableChangeDataFeed"])
}

func TestColumn_IsNullable(t *testing.T) {
	yes, no := true, false
	assert.True(t, config.Column{}.IsNullable(), "unset nullable means true")
	assert.True(t, config.Column{Nullable: &yes}.IsNullable())
	assert.False(t, config.Column{Nullable: &no}.IsNullable())
}
