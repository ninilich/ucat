package output

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ninilich/ucat/internal/change"
	"github.com/ninilich/ucat/internal/config"
)

// sampleChanges returns one change per Kind.
func sampleChanges() map[change.Kind]change.Change {
	p := change.TablePath{Catalog: "c", Schema: "s", Name: "t"}
	create := &change.CreateTableChange{Base: change.Base{Table: p}, Def: config.Table{Name: "t"}}
	return map[change.Kind]change.Change{
		change.AddColumn:          &change.AddColumnChange{Base: change.Base{Table: p}},
		change.DropColumn:         &change.DropColumnChange{Base: change.Base{Table: p}},
		change.AlterColumn:        &change.AlterColumnChange{Base: change.Base{Table: p}},
		change.RenameColumn:       &change.RenameColumnChange{Base: change.Base{Table: p}},
		change.RenameTable:        &change.RenameTableChange{OldPath: p},
		change.SetTBLProperties:   &change.SetTBLPropertiesChange{Base: change.Base{Table: p}},
		change.UnsetTBLProperties: &change.UnsetTBLPropertiesChange{Base: change.Base{Table: p}},
		change.AlterClustering:    &change.AlterClusteringChange{Base: change.Base{Table: p}},
		change.AlterTableComment:  &change.AlterTableCommentChange{Base: change.Base{Table: p}},
		change.DropTable:          &change.DropTableChange{Base: change.Base{Table: p}},
		change.CreateTable:        create,
	}
}

// Invariant over every Kind: IsDestructive is true exactly for DropTable and DropColumn,
// and classify agrees (destroy for both).
// A new Kind without an explicit decision here fails the test.
func TestDestructiveClassification_invariantOverAllKinds(t *testing.T) {
	samples := sampleChanges()
	for ct := change.Kind(0); ct < change.KindCount; ct++ {
		ch, ok := samples[ct]
		require.True(t, ok, "Kind %d has no sample: decide whether it is destructive and add it", ct)
		require.Equal(t, ct, ch.Type())

		wantDestructive := ct == change.DropTable || ct == change.DropColumn
		assert.Equal(t, wantDestructive, ch.IsDestructive(), "IsDestructive of Kind %d", ct)

		wantDestroy := ct == change.DropTable || ct == change.DropColumn
		assert.Equal(t, wantDestroy, classify(ch) == kindDestroy, "classify destroy of Kind %d", ct)
		if ch.IsDestructive() {
			assert.Equal(t, kindDestroy, classify(ch), "destructive Kind %d must be kindDestroy", ct)
		}
	}
	assert.Len(t, samples, int(change.KindCount), "no stray samples")
}
