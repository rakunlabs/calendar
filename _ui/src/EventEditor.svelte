<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { X, Trash2, Save, Repeat2, LoaderCircle } from '@lucide/svelte';
  import { addDays, format } from 'date-fns';
  import { deleteEvent, saveEvent, type CalendarEvent } from './lib/api';
  import { dateInput, dayKey, localZone, toInstant } from './lib/calendar';

  let {
    event,
    day,
    hour,
    groups,
    onclose,
    onsaved,
  }: {
    event: CalendarEvent | null;
    day: Date;
    hour?: number;
    groups: string[];
    onclose: () => void;
    onsaved: (message: string) => void;
  } = $props();
  const initial = untrack(() => ({ event, day, hour }));
  const original = initial.event;
  const initialZone = original?.tz || localZone;
  const initialAllDay = original?.all_day ?? initial.hour === undefined;
  const initialTime = new Date(initial.day);
  initialTime.setHours(initial.hour ?? 9, 0, 0, 0);
  let dialog: HTMLDialogElement;
  let name = $state(original?.name || '');
  let description = $state(original?.description || '');
  let group = $state(original?.event_group || '');
  let zone = $state(initialZone);
  let allDay = $state(initialAllDay);
  let disabled = $state(original?.disabled ?? false);
  let start = $state(
    original
      ? dateInput(original.date_from, initialZone, initialAllDay)
      : initialAllDay
        ? dayKey(initial.day)
        : format(initialTime, "yyyy-MM-dd'T'HH:mm"),
  );
  let end = $state(
    original
      ? dateInput(original.date_to, initialZone, initialAllDay)
      : initialAllDay
        ? dayKey(addDays(initial.day, 1))
        : format(new Date(initialTime.getTime() + 3600000), "yyyy-MM-dd'T'HH:mm"),
  );
  const rules = ['', 'RRULE:FREQ=DAILY', 'RRULE:FREQ=WEEKLY', 'RRULE:FREQ=MONTHLY', 'RRULE:FREQ=YEARLY'];
  let recurrence = $state(rules.includes(original?.rrule || '') ? original?.rrule || '' : 'custom');
  let author = $state('');
  let busy = $state(false);
  let error = $state('');
  let confirmingDelete = $state(false);

  onMount(() => {
    dialog.showModal();
    try {
      author = localStorage.getItem('calendar.author') || '';
    } catch {
      /* Storage is optional. */
    }
  });

  function toggleAllDay() {
    if (allDay) {
      start = start.slice(0, 10);
      end = end.slice(0, 10);
      if (end <= start) end = format(addDays(new Date(`${start}T12:00`), 1), 'yyyy-MM-dd');
    } else {
      start = `${start.slice(0, 10)}T09:00`;
      end = `${start.slice(0, 10)}T10:00`;
    }
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (busy) return;
    error = '';
    try {
      if (!name.trim()) throw new Error('Give your event a name.');
      const date_from = toInstant(start, zone);
      const date_to = toInstant(end, zone);
      if (date_to <= date_from) throw new Error('The end must be after the start.');
      busy = true;
      await saveEvent(
        {
          ...original,
          id: original?.id || '',
          name: name.trim(),
          description: description.trim(),
          event_group: group.trim() || null,
          date_from,
          date_to,
          tz: zone,
          all_day: allDay,
          disabled,
          rrule: recurrence === 'custom' ? original?.rrule || '' : recurrence,
        },
        !!original,
        author.trim(),
      );
      try {
        localStorage.setItem('calendar.author', author.trim());
      } catch {
        /* Storage is optional. */
      }
      onsaved(original ? 'Event updated.' : 'Event added to your calendar.');
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not save the event. Please retry.';
    } finally {
      busy = false;
    }
  }

  async function remove() {
    if (!original || busy) return;
    busy = true;
    error = '';
    try {
      await deleteEvent(original.id);
      onsaved('Event deleted.');
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not delete the event. Please retry.';
    } finally {
      busy = false;
    }
  }
</script>

<dialog
  bind:this={dialog}
  class="event-dialog"
  aria-labelledby="editor-title"
  oncancel={(e) => {
    e.preventDefault();
    if (!busy) onclose();
  }}
>
  <form onsubmit={submit}>
    <header class="editor-header">
      <div>
        <h2 id="editor-title">{original ? 'Edit event' : 'A little room for something new.'}</h2>
        <p>
          {original ? 'Update the details on your calendar.' : 'Add an event and make it part of your day.'}
        </p>
      </div>
      <button
        type="button"
        class="icon-button"
        aria-label="Close event editor"
        disabled={busy}
        onclick={onclose}><X size={20} /></button
      >
    </header>
    <div class="editor-body">
      {#if original?.rrule}<div class="notice">
          <Repeat2 size={16} /><span
            >You are editing the entire recurring series, not just this occurrence.</span
          >
        </div>{/if}
      {#if error}<div class="error-message" role="alert">{error}</div>{/if}
      <label for="event-name">Event name</label>
      <input
        id="event-name"
        class="title-input"
        bind:value={name}
        required
        maxlength="200"
        placeholder="What is happening?"
        disabled={busy}
      />
      <div class="form-row">
        <div>
          <label for="event-group">Calendar group</label><input
            id="event-group"
            list="event-groups"
            bind:value={group}
            placeholder="e.g. Team, Holidays"
            disabled={busy}
          /><datalist id="event-groups"
            >{#each groups.filter((g) => g !== 'Ungrouped') as g}<option value={g}></option>{/each}</datalist
          >
        </div>
        <div>
          <label for="event-repeat">Repeat</label><select
            id="event-repeat"
            bind:value={recurrence}
            disabled={busy}
            ><option value="">Does not repeat</option><option value="RRULE:FREQ=DAILY">Every day</option
            ><option value="RRULE:FREQ=WEEKLY">Every week</option><option value="RRULE:FREQ=MONTHLY"
              >Every month</option
            ><option value="RRULE:FREQ=YEARLY">Every year</option
            >{#if original?.rrule && !rules.includes(original.rrule)}<option value="custom"
                >Keep existing rule</option
              >{/if}</select
          >
        </div>
      </div>
      {#if recurrence === 'custom'}<p class="field-hint rule-preview">{original?.rrule}</p>{/if}
      <label class="checkbox-label"
        ><input type="checkbox" bind:checked={allDay} onchange={toggleAllDay} disabled={busy} />All-day event</label
      >
      <div class="form-row">
        <div>
          <label for="event-start">Starts</label><input
            id="event-start"
            type={allDay ? 'date' : 'datetime-local'}
            bind:value={start}
            required
            disabled={busy}
          />
        </div>
        <div>
          <label for="event-end">Ends {allDay ? '(exclusive)' : ''}</label><input
            id="event-end"
            type={allDay ? 'date' : 'datetime-local'}
            bind:value={end}
            required
            disabled={busy}
          />
        </div>
      </div>
      {#if allDay}<p class="field-hint">
          For a one-day event, choose the following day as the end date.
        </p>{/if}
      <label for="event-zone">Time zone</label><input
        id="event-zone"
        bind:value={zone}
        required
        list="time-zones"
        disabled={busy}
      /><datalist id="time-zones"
        ><option>UTC</option><option>Europe/Istanbul</option><option>Europe/Amsterdam</option><option
          >Europe/London</option
        ><option>America/New_York</option><option>Asia/Tokyo</option></datalist
      >
      <label for="event-description">Notes <span class="optional">optional</span></label><textarea
        id="event-description"
        rows="3"
        bind:value={description}
        placeholder="Add some context, a location, or a reminder…"
        disabled={busy}></textarea>
      <details class="advanced">
        <summary>Additional settings</summary><label for="event-author"
          >Updated by <span class="optional">audit label, not authentication</span></label
        ><input id="event-author" bind:value={author} placeholder="Your name" disabled={busy} /><label
          class="checkbox-label"
          ><input type="checkbox" bind:checked={disabled} disabled={busy} />Disable this event</label
        >
      </details>
    </div>
    <footer class="editor-footer">
      {#if confirmingDelete}
        <div class="delete-confirm" role="alert">
          <p>Delete {original?.rrule ? 'this entire series' : 'this event'}? This cannot be undone.</p>
          <div class="flex gap-2">
            <button
              type="button"
              class="secondary-button"
              disabled={busy}
              onclick={() => (confirmingDelete = false)}>Keep event</button
            ><button type="button" class="danger-button" disabled={busy} onclick={remove}
              >{busy ? 'Deleting…' : 'Delete permanently'}</button
            >
          </div>
        </div>
      {:else}
        {#if original}<button
            type="button"
            class="delete-button"
            disabled={busy}
            onclick={() => (confirmingDelete = true)}><Trash2 size={16} />Delete</button
          >{/if}
        <div class="flex gap-2 ml-auto">
          <button type="button" class="secondary-button" disabled={busy} onclick={onclose}>Cancel</button
          ><button type="submit" class="primary-button" disabled={busy}
            >{#if busy}<LoaderCircle size={16} class="spin" />{:else}<Save size={16} />{/if}{busy
              ? 'Saving…'
              : original
                ? 'Save changes'
                : 'Create event'}</button
          >
        </div>
      {/if}
    </footer>
  </form>
</dialog>
