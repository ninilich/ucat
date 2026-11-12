package backend_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ninilich/ucat/internal/backend"
)

func TestLiveTable_HasClustering(t *testing.T) {
	assert.False(t, (&backend.LiveTable{}).HasClustering())
	assert.True(t, (&backend.LiveTable{ClusterAuto: true}).HasClustering())
	assert.True(t, (&backend.LiveTable{Clustering: []string{"a"}}).HasClustering())
}

func TestLiveTable_NestedClusteringField(t *testing.T) {
	assert.Equal(t, "", (&backend.LiveTable{}).NestedClusteringField())
	assert.Equal(t, "", (&backend.LiveTable{Clustering: []string{"a", "b"}}).NestedClusteringField())
	assert.Equal(t, "b.c", (&backend.LiveTable{Clustering: []string{"a", "b.c", "d.e"}}).NestedClusteringField(), "the first one")
}
