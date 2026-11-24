package models

import "strings"

type WorkspaceNodePathEntry struct {
	ID       string
	ParentID *string
	Title    string
}

func IsHiddenWorkspaceName(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), ".")
}

// HiddenWorkspaceNodeIDs includes descendants so a hidden directory cannot expose its children.
func HiddenWorkspaceNodeIDs(nodes []WorkspaceNodePathEntry) map[string]struct{} {
	var pending []string
	for _, node := range nodes {
		if IsHiddenWorkspaceName(node.Title) {
			pending = append(pending, node.ID)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	children := make(map[string][]string)
	for _, node := range nodes {
		if node.ParentID != nil {
			children[*node.ParentID] = append(children[*node.ParentID], node.ID)
		}
	}
	hidden := make(map[string]struct{}, len(pending))
	for len(pending) > 0 {
		last := len(pending) - 1
		id := pending[last]
		pending = pending[:last]
		if _, visited := hidden[id]; visited {
			continue
		}
		hidden[id] = struct{}{}
		pending = append(pending, children[id]...)
	}
	return hidden
}
