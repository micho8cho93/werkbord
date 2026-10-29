-- Generated workspaces and collections are retired; execution and memory are untouched.
DROP TABLE personal_workspace_requests;
DROP TABLE personal_workspace_history;
DROP TABLE personal_workspace_collections;
DROP TABLE personal_records;
DROP TABLE personal_collection_state;
DROP TABLE personal_workspaces;
DROP TABLE personal_collections;

DELETE FROM assistant_actions WHERE operation IN ('propose_personal_workspace', 'propose_delete_personal_workspace');

-- Preserve the nine-position layout and every retained navigation/workflow button.
UPDATE settings SET value = json_set(value,
 '$.slots', json((SELECT json_group_array(CASE
   WHEN json_extract(slot.value, '$.button') GLOB 'pws_*' THEN NULL
   ELSE json(slot.value) END)
   FROM json_each(settings.value, '$.slots') AS slot)),
 '$.version', COALESCE(json_extract(value, '$.version'), 0) + 1)
WHERE key LIKE 'phone-deck.%' AND json_type(value, '$.slots') = 'array'
 AND EXISTS (SELECT 1 FROM json_each(settings.value, '$.slots') AS slot
             WHERE json_extract(slot.value, '$.button') GLOB 'pws_*');
