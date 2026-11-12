package uc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/backend"
)

// listTables through a fake workspace: only MANAGED tables, columns by Position (not response order),
// system properties dropped, user properties kept, partitions by index.
func TestListTables_ManagedOnlySortedSystemPropsFiltered(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasPrefix(r.URL.Path, "/api/2.1/unity-catalog/tables") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"tables":[
		 {"name":"managed","catalog_name":"prod","schema_name":"silver","table_type":"MANAGED",
		  "columns":[
		   {"name":"c","type_text":"INT","position":2,"nullable":true},
		   {"name":"a","type_text":"BIGINT","position":0,"nullable":false,"partition_index":2},
		   {"name":"b","type_text":"STRING","position":1,"nullable":true,"partition_index":1}],
		  "properties":{"clusterByAuto":"true","clusteringColumns":"[[\"a\"]]","clusteringColumnsIndexed":"x","team":"data"}},
		 {"name":"ext","catalog_name":"prod","schema_name":"silver","table_type":"EXTERNAL"},
		 {"name":"v","catalog_name":"prod","schema_name":"silver","table_type":"VIEW"}]}`))
	}))
	t.Cleanup(srv.Close)
	b := newTestBackend(t, srv, "wh")

	got, err := b.ListTables(t.Context(), backend.SchemaRef{UCCatalog: litProd, UCSchema: litSilver})
	require.NoError(t, err)
	assert.Contains(t, query, "catalog_name=prod")
	assert.Contains(t, query, "schema_name=silver")

	require.Len(t, got, 1, "EXTERNAL and VIEW must be skipped")
	tbl := got[0]
	assert.Equal(t, "managed", tbl.Name)
	require.Len(t, tbl.Columns, 3)
	assert.Equal(t, []string{"a", "b", "c"}, []string{tbl.Columns[0].Name, tbl.Columns[1].Name, tbl.Columns[2].Name})
	assert.False(t, tbl.Columns[0].Nullable)
	assert.True(t, tbl.Columns[1].Nullable)
	assert.Equal(t, []string{"b", "a"}, tbl.Partitioning)
	assert.Equal(t, map[string]string{litTeam: litData}, tbl.Properties, "system properties are not user properties")
	assert.Equal(t, "prod", tbl.Catalog)
	assert.Equal(t, "silver", tbl.Schema)
}
