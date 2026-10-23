package main

import (
	"encoding/json"
	"regexp"
	"strings"

	"devboard/internal/domain"
)

func personalFixtureCall(name string, args any) string {
	raw, _ := json.Marshal(map[string]any{"id": "personal", "name": name, "arguments": args})
	return "```werkbord-call\n" + string(raw) + "\n```"
}
func personalFixtureReply(prompt, deletionMode string) (string, bool) {
	// A model change may include the old transcript. Follow only the current
	// result envelope or the last user request, as a real provider must.
	if i := strings.LastIndex(prompt, "<werkbord-results>"); i >= 0 {
		prompt = prompt[i:]
	}
	low := strings.ToLower(prompt)
	if strings.Contains(prompt, `name="propose_delete_personal_workspace"`) {
		return "Review the deletion preview. Nothing is deleted until you confirm.", true
	}
	if strings.Contains(prompt, `name="propose_personal_workspace"`) {
		return "Review the workspace preview and confirm to save it.", true
	}
	if strings.Contains(prompt, `name="list_personal_workspaces"`) {
		if strings.Contains(prompt, `"pws_`) {
			return "Your workspace is already saved. Open it to request a design change.", true
		}
		return personalFixtureCall("propose_personal_workspace", fixtureTodo()), true
	}
	if strings.Contains(prompt, `name="get_personal_workspace"`) {
		match := regexp.MustCompile(`(?s)<werkbord-result[^>]*name="get_personal_workspace"[^>]*>\n(.*?)\n</werkbord-result>`).FindStringSubmatch(prompt)
		if len(match) != 2 {
			return "Could not read the saved workspace.", true
		}
		var snap domain.PersonalSnapshot
		if json.Unmarshal([]byte(match[1]), &snap) != nil || snap.Workspace == nil {
			return "Could not read the saved workspace.", true
		}
		if deletionMode != "" {
			return personalFixtureCall("propose_delete_personal_workspace", map[string]any{"workspaceId": snap.Workspace.ID, "revision": snap.Workspace.Revision, "mode": deletionMode}), true
		}
		ch := domain.PersonalChange{WorkspaceID: snap.Workspace.ID, Revision: snap.Workspace.Revision, Workspace: snap.Workspace.PersonalWorkspaceSpec}
		for _, c := range snap.Collections {
			ch.Collections = append(ch.Collections, domain.PersonalCollectionChange{ID: c.ID, Revision: c.Revision, Spec: c.PersonalCollectionSpec})
		}
		ch.Workspace.Definition.Views = append(ch.Workspace.Definition.Views, domain.PersonalView{ID: "urgent", Name: "Urgent tasks", Kind: "list", CollectionID: ch.Collections[0].ID, Fields: []string{"title", "priority"}, Filters: []domain.PersonalFilter{{Field: "priority", Value: "Urgent"}}})
		ch.Workspace.Definition.Layout.Children = []domain.PersonalNode{{Type: "view", ViewID: "progress"}, {Type: "view", ViewID: "urgent"}, {Type: "view", ViewID: "tasks"}}
		return personalFixtureCall("propose_personal_workspace", ch), true
	}

	if deletionMode != "" {
		ids := regexp.MustCompile(`pws_[a-z2-7]{16}`).FindAllString(prompt, -1)
		if len(ids) > 0 {
			return personalFixtureCall("get_personal_workspace", map[string]any{"workspaceId": ids[len(ids)-1]}), true
		}
	}
	if strings.Contains(low, "urgent tasks") {
		ids := regexp.MustCompile(`pws_[a-z2-7]{16}`).FindAllString(prompt, -1)
		id := ""
		if len(ids) > 0 {
			id = ids[len(ids)-1]
		}
		return personalFixtureCall("get_personal_workspace", map[string]any{"workspaceId": id}), true
	}
	if strings.Contains(low, "weekly to-do") {
		return personalFixtureCall("list_personal_workspaces", map[string]any{}), true
	}
	return "", false
}
func fixtureTodo() domain.PersonalChange {
	return domain.PersonalChange{Workspace: domain.PersonalWorkspaceSpec{Name: "Weekly to-dos", Icon: "check", UISchemaVersion: 1,
		Visibility: domain.PersonalVisibility{Universal: true, Individual: true, TeamWorkspaceIDs: []string{"team:design"}}, CollectionIDs: []string{"tasks"},
		Definition: domain.PersonalUI{Views: []domain.PersonalView{
			{ID: "tasks", Name: "Weekly tasks", Kind: "checklist", CollectionID: "tasks", Fields: []string{"title", "deadline", "priority"}, CheckedField: "done", Operations: []string{"create", "update"}},
			{ID: "progress", Name: "Progress", Kind: "progress", CollectionID: "tasks", Fields: []string{"done"}, CheckedField: "done"},
		}, Layout: domain.PersonalNode{Type: "stack", Children: []domain.PersonalNode{{Type: "view", ViewID: "tasks"}, {Type: "view", ViewID: "progress"}}}},
	}, Collections: []domain.PersonalCollectionChange{{ID: "tasks", Spec: domain.PersonalCollectionSpec{Name: "Weekly tasks", SchemaVersion: 1, Fields: []domain.PersonalField{
		{Key: "title", Name: "Task", Type: "text", Required: true}, {Key: "deadline", Name: "Deadline", Type: "date"}, {Key: "priority", Name: "Priority", Type: "select", Options: []string{"Low", "Normal", "Urgent"}}, {Key: "done", Name: "Done", Type: "boolean"},
	}}}}}
}
