package uc

import (
	"testing"

	"github.com/databricks/databricks-sdk-go/service/catalog"
	"github.com/stretchr/testify/assert"
)

// mapTableInfo sorts a copy of the columns: the SDK response is left as it was, so partition
// columns with equal indexes keep the response order, not the column position order.
func TestMapTableInfo_DoesNotReorderTheResponse(t *testing.T) {
	cols := []catalog.ColumnInfo{
		{Name: "b", TypeText: typINT, Position: 1, PartitionIndex: 1},
		{Name: "a", TypeText: typINT, Position: 0, PartitionIndex: 1},
	}
	got := mapTableInfo(catalog.TableInfo{Name: "t", Columns: cols})
	assert.Equal(t, "b", cols[0].Name, "the input slice is not sorted in place")
	assert.Equal(t, "a", got.Columns[0].Name)
	assert.Equal(t, []string{"b", "a"}, got.Partitioning, "equal partition indexes keep the response order")
}
