package repository

import (
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"gorm.io/gorm"
)

// excludeHiddenWorkspaceNodes filters dot-prefixed nodes and all their descendants
// before counting or paging public-facing document results.
func excludeHiddenWorkspaceNodes(query *gorm.DB, nodeAlias string) *gorm.DB {
	const hiddenTree = "hidden_workspace_nodes"
	nodeTable := tableName(models.Node{})
	columns := models.NodeColumns
	hiddenIDsSQL := "WITH RECURSIVE " + hiddenTree + " (" + columns.NodeID + ", " + columns.SpaceID + ") AS (" +
		"SELECT " + columns.NodeID + ", " + columns.SpaceID + " FROM " + nodeTable + " WHERE TRIM(" + columns.Title + ") LIKE ? " +
		"UNION SELECT child." + columns.NodeID + ", child." + columns.SpaceID + " FROM " + nodeTable + " AS child " +
		"JOIN " + hiddenTree + " AS hidden ON child." + columns.ParentNodeID + " = hidden." + columns.NodeID +
		" AND child." + columns.SpaceID + " = hidden." + columns.SpaceID +
		") SELECT " + columns.NodeID + " FROM " + hiddenTree
	hiddenIDs := query.Session(&gorm.Session{NewDB: true}).Raw(hiddenIDsSQL, ".%")
	return query.Where(qualifiedColumn(nodeAlias, columns.NodeID)+" NOT IN (?)", hiddenIDs)
}
