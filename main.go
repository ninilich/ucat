// Command ucat manages Unity Catalog tables declaratively: it diffs YAML definitions
// against a Databricks workspace and plans, applies or pulls the changes.
package main

import "github.com/ninilich/ucat/cmd"

func main() {
	cmd.Execute()
}
