package live_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
	"github.com/ninilich/ucat/internal/backend/backendtest"
	"github.com/ninilich/ucat/internal/config"
	"github.com/ninilich/ucat/internal/live"
)

func resolvedWith(wh string, schemas ...string) config.ResolvedConfig {
	r := config.ResolvedConfig{WarehouseID: wh}
	for _, s := range schemas {
		r.Schemas = append(r.Schemas, config.ResolvedSchema{UCCatalog: "c", UCSchema: s})
	}
	return r
}

func TestCheckLiveAllOK(t *testing.T) {
	res := live.CheckLive(context.Background(), &backendtest.MockUCBackend{}, resolvedWith("wh1", "a", "b"))
	require.Len(t, res, 3)
	assert.Equal(t, "schema c.a exists", res[0].Line)
	assert.Equal(t, "schema c.b exists", res[1].Line)
	assert.Equal(t, "warehouse wh1 reachable", res[2].Line)
	for _, r := range res {
		assert.True(t, r.OK)
		assert.False(t, r.Failed())
	}
}

func TestCheckLiveNoWarehouseNoResult(t *testing.T) {
	res := live.CheckLive(context.Background(), &backendtest.MockUCBackend{}, resolvedWith("", "a"))
	require.Len(t, res, 1)
	assert.Equal(t, "schema c.a exists", res[0].Line)
}

func TestCheckLiveSchemaFailureDoesNotStopNext(t *testing.T) {
	bad := backend.SchemaRef{UCCatalog: "c", UCSchema: "a"}
	fb := &failingBackend{MockUCBackend: &backendtest.MockUCBackend{}, failFor: map[backend.SchemaRef]bool{bad: true}}
	res := live.CheckLive(context.Background(), fb, resolvedWith("wh1", "a", "b"))
	require.Len(t, res, 3)
	assert.False(t, res[0].OK)
	assert.Equal(t, "schema c.a: boom a", res[0].Line)
	assert.True(t, res[0].Failed())
	assert.True(t, res[1].OK)
	assert.True(t, res[2].OK)
}

func TestCheckLiveWarehouseFailure(t *testing.T) {
	m := &backendtest.MockUCBackend{}
	m.SetWarehouseError(errors.New("down"))
	res := live.CheckLive(context.Background(), m, resolvedWith("wh1"))
	require.Len(t, res, 1)
	assert.Equal(t, "warehouse wh1: down", res[0].Line)
	assert.Equal(t, "warehouse wh1", res[0].Name)
	assert.True(t, res[0].Failed())
}

func TestCheckLiveDiffErrorFailsSchemaButKeepsOK(t *testing.T) {
	// A column rename_from that exists neither in the live table nor as the target is a diff error.
	r := resolvedWith("", "a")
	r.Tables = []config.Table{{Name: "t", ResolvedCatalog: "c", ResolvedSchema: "a",
		Columns: []config.Column{{Name: "id", Type: "INT", RenameFrom: "typo"}}}}
	m := &backendtest.MockUCBackend{}
	m.SetFixture(backend.SchemaRef{UCCatalog: "c", UCSchema: "a"}, []*backend.LiveTable{
		backendtest.LiveOf(config.Table{Name: "t", Columns: []config.Column{{Name: "other", Type: "INT"}}}),
	})
	res := live.CheckLive(context.Background(), m, r)
	require.Len(t, res, 1)
	assert.True(t, res[0].OK)
	assert.True(t, res[0].Diags.HasError(), "%v", res[0].Diags)
	assert.Equal(t, "schema c.a", res[0].Name, "the final error of validate -t names the schema")
	assert.True(t, res[0].Failed())
}
