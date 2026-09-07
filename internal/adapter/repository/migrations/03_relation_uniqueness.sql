-- Prevent concurrent inserts between cleanup and index creation.
LOCK TABLE calendar_relations IN SHARE ROW EXCLUSIVE MODE;

-- NULL targets are equal for assignment identity. Retain the latest metadata.
DELETE FROM calendar_relations
WHERE ctid IN (
    SELECT row_id FROM (
        SELECT ctid AS row_id,
               row_number() OVER (
                   PARTITION BY entity, event_group, event_id
                   ORDER BY updated_at DESC NULLS LAST, ctid DESC
               ) AS position
        FROM calendar_relations
    ) duplicates
    WHERE position > 1
);

CREATE UNIQUE INDEX calendar_relations_group_only_unique
    ON calendar_relations (entity, event_group) WHERE event_id IS NULL;
CREATE UNIQUE INDEX calendar_relations_event_only_unique
    ON calendar_relations (entity, event_id) WHERE event_group IS NULL;
-- Preserve legacy targetless rows, but make their identity unique too.
CREATE UNIQUE INDEX calendar_relations_targetless_unique
    ON calendar_relations (entity) WHERE event_group IS NULL AND event_id IS NULL;
