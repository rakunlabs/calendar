<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { X } from '@lucide/svelte';
  import {
    addRelation,
    deleteRelation,
    downloadICS,
    icsURL,
    importICS,
    type CalendarEvent,
    type Relation,
  } from './lib/api';

  let {
    relations,
    entities,
    events,
    groups,
    loading,
    error: catalogError,
    selectedEntity,
    onretry,
    onchange,
    onclose,
  }: {
    relations: Relation[];
    entities: string[];
    events: CalendarEvent[];
    groups: string[];
    loading: boolean;
    error: string;
    selectedEntity: string;
    onretry: () => void;
    onchange: () => void;
    onclose: () => void;
  } = $props();
  let dialog: HTMLDialogElement;
  let tab = $state('assignments');
  let entity = $state(untrack(() => selectedEntity));
  let target = $state('group');
  let group = $state('');
  let eventID = $state('');
  let eventSearch = $state('');
  let author = $state('');
  let busy = $state(false);
  let error = $state('');
  let message = $state('');
  let files = $state<FileList>();
  let importGroup = $state('');
  let zone = $state('');
  let exportEntity = $state(untrack(() => selectedEntity));
  let exportGroup = $state('');
  let year = $state<number>();
  let copyMessage = $state('');
  const subscription = $derived(icsURL(exportEntity, exportGroup));
  const matchingEvents = $derived(
    events.filter(
      (e) =>
        e.id === eventID ||
        `${e.name} ${e.event_group || ''} ${e.id}`.toLowerCase().includes(eventSearch.toLowerCase()),
    ),
  );
  const assignments = $derived(relations.filter((r) => r.entity === entity));
  onMount(() => dialog.showModal());

  async function run(action: () => Promise<unknown>, success: string, refresh = false) {
    busy = true;
    error = '';
    message = '';
    try {
      await action();
      message = success;
      if (refresh) onchange();
    } catch (e) {
      error = e instanceof Error ? e.message : 'The request failed. Please retry.';
    } finally {
      busy = false;
    }
  }
  function add() {
    return run(
      () =>
        addRelation(
          {
            entity,
            event_group: target === 'group' ? group : null,
            event_id: target === 'event' ? eventID : null,
          },
          author,
        ),
      'Assignment added. Refreshing calendars and assignments.',
      true,
    );
  }
  async function download() {
    await run(async () => {
      const blob = await downloadICS(exportEntity, exportGroup, year === undefined ? '' : String(year));
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `calendar-${(exportEntity || 'all').replace(/[^a-z0-9_-]/gi, '_').slice(0, 60)}.ics`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    }, 'ICS download ready.');
  }
  async function copy() {
    copyMessage = '';
    try {
      await navigator.clipboard.writeText(subscription);
      copyMessage = 'Subscription URL copied.';
    } catch {
      copyMessage = 'Clipboard unavailable. Select and copy the URL below.';
    }
  }
</script>

<dialog
  bind:this={dialog}
  class="event-dialog tools-dialog"
  aria-labelledby="tools-title"
  oncancel={(e) => {
    e.preventDefault();
    if (!busy) onclose();
  }}
>
  <header class="editor-header">
    <div>
      <h2 id="tools-title">Calendar tools</h2>
      <p>Assign events, import calendars, and share subscriptions.</p>
    </div>
    <button class="icon-button" aria-label="Close calendar tools" disabled={busy} onclick={onclose}
      ><X size={20} /></button
    >
  </header>
  <div class="tools-navigation" aria-label="Calendar tools sections">
    <button
      class="secondary-button"
      aria-pressed={tab === 'assignments'}
      disabled={busy}
      onclick={() => {
        tab = 'assignments';
        error = '';
        message = '';
      }}>Assignments</button
    >
    <button
      class="secondary-button"
      aria-pressed={tab === 'ics'}
      disabled={busy}
      onclick={() => {
        tab = 'ics';
        error = '';
        message = '';
      }}>Import & export</button
    >
  </div>
  <div class="editor-body tools-body" aria-busy={busy}>
    {#if error}<p class="error-message" role="alert">{error}</p>{/if}
    {#if message}<p class="notice" role="status">{message}</p>{/if}
    {#if catalogError}<div class="error-message" role="alert">
        {catalogError}
        <button class="secondary-button" disabled={busy || loading} onclick={onretry}>Retry catalogs</button>
      </div>{/if}
    {#if loading}<p class="field-hint" role="status">Loading calendars and assignments...</p>{/if}
    <datalist id="tools-entities"
      >{#each entities as name}<option value={name}></option>{/each}</datalist
    >
    <datalist id="tools-groups"
      >{#each groups as name}<option value={name}></option>{/each}</datalist
    >
    {#if tab === 'assignments'}
      <p class="field-hint">
        Entities collect whole groups or individual events. These assignments filter calendars; they do not
        grant or restrict access.
      </p>
      <form
        onsubmit={(e) => {
          e.preventDefault();
          add();
        }}
      >
        <fieldset disabled={busy || loading || !!catalogError}>
          <label for="assignment-entity">Entity name</label>
          <input
            id="assignment-entity"
            list="tools-entities"
            bind:value={entity}
            required
            placeholder="Choose or enter a new entity"
          />
          <p class="field-hint">New names are created with their first assignment.</p>
          <label for="assignment-target">Assign</label>
          <select id="assignment-target" bind:value={target}
            ><option value="group">Whole group</option><option value="event">Individual event</option></select
          >
          {#if target === 'group'}
            <label for="assignment-group">Assignment group</label><input
              id="assignment-group"
              list="tools-groups"
              bind:value={group}
              required
              placeholder="Choose or enter a group name"
            />
            <p class="field-hint">Includes current and future events in this group.</p>
          {:else}
            <label for="assignment-search">Find an event</label><input
              id="assignment-search"
              bind:value={eventSearch}
              type="search"
              placeholder="Search name, group, or ID"
            />
            <label for="assignment-event">Assignment event</label><select
              id="assignment-event"
              bind:value={eventID}
              required
              ><option value="">Choose an event</option>{#each matchingEvents as event}<option
                  value={event.id}
                  >{event.name || 'Untitled event'} · {event.event_group || 'No group'} · {event.id}{event.disabled
                    ? ' (disabled)'
                    : ''}</option
                >{/each}</select
            >
            <p class="field-hint">
              All saved events are available, including disabled events and dates outside this view.
            </p>
          {/if}
          <label for="assignment-author">Audit label (optional)</label><input
            id="assignment-author"
            bind:value={author}
            placeholder="Your name"
          />
          <button
            class="primary-button tools-action"
            disabled={!entity.trim() || !(target === 'group' ? group.trim() : eventID)}
            type="submit">{busy ? 'Saving...' : 'Add assignment'}</button
          >
        </fieldset>
      </form>
      <section class="tools-section" aria-labelledby="assignments-title">
        <h3 id="assignments-title">{entity ? `Assignments for ${entity}` : 'Existing assignments'}</h3>
        {#if !loading && !catalogError && !assignments.length}<p class="field-hint">
            {entity
              ? 'No assignments for this entity. Add a group or event above.'
              : 'Choose an entity above to view its assignments.'}
          </p>{/if}
        <ul class="assignment-list">
          {#each assignments as relation}
            <li>
              <div>
                {#if relation.event_group !== null}<strong>Group: {relation.event_group}</strong>{/if}
                {#if relation.event_group !== null && relation.event_id !== null}<span class="field-hint"
                    >OR</span
                  >{/if}
                {#if relation.event_id !== null}<strong
                    >Event: {events.find((e) => e.id === relation.event_id)?.name ||
                      relation.event_id}</strong
                  ><span class="field-hint">{relation.event_id}</span>{/if}
                {#if relation.event_group !== null && relation.event_id !== null}<p class="field-hint">
                    Combined rule. Removing it removes both targets from this rule only.
                  </p>{/if}
              </div>
              <button
                class="secondary-button"
                disabled={busy || loading || !!catalogError}
                aria-label={`Remove assignment for ${relation.event_group || relation.event_id}`}
                onclick={() =>
                  run(
                    () => deleteRelation(relation),
                    'Assignment removed. Refreshing calendars and assignments.',
                    true,
                  )}>Remove</button
              >
            </li>
          {/each}
        </ul>
      </section>
    {:else}
      <section aria-labelledby="import-title">
        <h3 id="import-title">Import ICS</h3>
        <p class="field-hint">
          Import events from a file. This does not create entity assignments; use Assignments afterward if
          needed.
        </p>
        <form
          onsubmit={(e) => {
            e.preventDefault();
            if (files?.[0])
              run(
                () => importICS(files![0], importGroup, zone, author),
                'Import completed. Refreshing calendars. Entity assignments were not changed.',
                true,
              );
          }}
        >
          <fieldset disabled={busy}>
            <label for="ics-file">ICS file (up to 9 MiB)</label><input
              id="ics-file"
              type="file"
              accept=".ics,text/calendar"
              bind:files
              required
            />
            <label for="ics-group">Import group (optional)</label><input
              id="ics-group"
              list="tools-groups"
              bind:value={importGroup}
              placeholder="Leave blank to import without a group"
            />
            <label for="ics-zone">Import time zone (optional)</label><input
              id="ics-zone"
              bind:value={zone}
              placeholder="e.g. Europe/Istanbul"
            />
            <p class="field-hint">
              Used for dates without a time zone; leave blank for UTC. Unsupported calendar exceptions are
              reported as errors.
            </p>
            <label for="import-author">Audit label (optional)</label><input
              id="import-author"
              bind:value={author}
            />
            <button class="primary-button tools-action" type="submit" disabled={!files?.length}
              >{busy ? 'Working...' : 'Import file'}</button
            >
          </fieldset>
        </form>
      </section>
      <section class="tools-section" aria-labelledby="export-title">
        <h3 id="export-title">Download & subscribe</h3>
        <fieldset disabled={busy}>
          <label for="export-entity">Export entity</label><select
            id="export-entity"
            bind:value={exportEntity}
            onchange={() => (copyMessage = '')}
            ><option value="">All entities (all events)</option
            >{#each [...new Set([...entities, ...(exportEntity ? [exportEntity] : [])])] as name}<option
                value={name}>{name}</option
              >{/each}</select
          >
          <label for="export-group">Export group scope</label><select
            id="export-group"
            bind:value={exportGroup}
            onchange={() => (copyMessage = '')}
            ><option value="">All groups</option>{#each groups as name}<option value={name}>{name}</option
              >{/each}</select
          >
          <p class="notice tools-action">
            Exports include enabled events matching both scope controls above. Calendar search, hidden groups,
            and “Show disabled events” do not affect exports.
          </p>
          <label for="export-year">Download year (optional)</label><input
            id="export-year"
            type="number"
            min="1"
            max="9999"
            step="1"
            bind:value={year}
            placeholder="Rolling default"
          />
          <p class="field-hint">
            Default: previous year, current year, and next two years. A year selects event series, not an
            exact calendar-view range.
          </p>
          <button
            class="secondary-button tools-action"
            disabled={year !== undefined && (!Number.isInteger(year) || year < 1 || year > 9999)}
            onclick={download}>Download ICS</button
          >
          <button class="secondary-button tools-action" onclick={copy}>Copy subscription URL</button>
        </fieldset>
        <label for="subscription-url">Subscription URL (rolling years)</label><input
          id="subscription-url"
          readonly
          value={subscription}
          onclick={(e) => e.currentTarget.select()}
        />
        {#if copyMessage}<p class="field-hint" role="status">{copyMessage}</p>{/if}
        <p class="field-hint">
          The URL must be reachable by your calendar provider. It is not an authorization token or access
          restriction. The download year is never included in subscriptions.
        </p>
      </section>
    {/if}
  </div>
  <footer class="editor-footer">
    <button class="secondary-button" disabled={busy} onclick={onclose}>Close</button>
  </footer>
</dialog>
