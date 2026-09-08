<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { X, Trash2, Save, Repeat2, LoaderCircle } from '@lucide/svelte';
  import { format } from 'date-fns';
  import { deleteEvent, saveEvent, type CalendarEvent } from './lib/api';
  import { dateInput, dayKey, localZone, toInstant } from './lib/calendar';
  import { moveEditorEnd, shiftEditorDate } from './lib/editorDates';
  import { editOccurrence, editSeries, editorTimingZone, hasExceptions } from './lib/recurrence';

  let {
    event,
    master = event,
    occurrence,
    scope = 'series',
    onscope,
    day,
    hour,
    endDay,
    groups,
    groupsLoading = false,
    groupsError = '',
    onretrygroups = () => {},
    onclose,
    onsaved,
  }: {
    event: CalendarEvent | null;
    master?: CalendarEvent | null;
    occurrence?: CalendarEvent;
    scope?: 'occurrence' | 'series';
    onscope?: (scope: 'occurrence' | 'series') => void;
    day: Date;
    hour?: number;
    endDay?: Date;
    groups: string[];
    groupsLoading?: boolean;
    groupsError?: string;
    onretrygroups?: () => void;
    onclose: () => void;
    onsaved: (message: string) => void;
  } = $props();
  const initial = untrack(() => ({ event, master, day, hour, endDay }));
  const original = initial.event;
  const { zone: initialZone, locked: unsupportedZone } = editorTimingZone(original, initial.master);
  const timingLocked = $derived(unsupportedZone || (scope === 'series' && !!master && hasExceptions(master)));
  const initialAllDay = original?.all_day ?? initial.hour === undefined;
  const initialTime = new Date(initial.day);
  if (initial.hour !== undefined) {
    const minutes = Math.round(initial.hour * 60);
    initialTime.setHours(Math.floor(minutes / 60), minutes % 60, 0, 0);
  } else {
    initialTime.setHours(9, 0, 0, 0);
  }
  const initialEnd =
    initial.hour !== undefined && initial.endDay && initial.endDay > initialTime
      ? initial.endDay
      : new Date(initialTime.getTime() + 3600000);
  const zones = [
    ...new Set([
      'UTC',
      localZone,
      initialZone,
      'Europe/Istanbul',
      'Europe/Amsterdam',
      'Europe/London',
      'America/New_York',
      'America/Chicago',
      'America/Denver',
      'America/Los_Angeles',
      'Asia/Dubai',
      'Asia/Kolkata',
      'Asia/Singapore',
      'Asia/Tokyo',
      'Australia/Sydney',
    ]),
  ];
  let dialog: HTMLDialogElement;
  let name = $state(original?.name || '');
  let description = $state(original?.description || '');
  let group = $state(original?.event_group || '');
  let newGroup = $state(false);
  const groupOptions = $derived([...new Set([...groups, ...(group ? [group] : [])])].sort());
  let zone = $state(initialZone);
  let allDayChoice = $state(initialAllDay);
  let disabled = $state(original?.disabled ?? false);
  const initialStartValue = original
    ? dateInput(original.date_from, initialZone, initialAllDay)
    : initialAllDay
      ? dayKey(initial.day)
      : format(initialTime, "yyyy-MM-dd'T'HH:mm");
  const initialEndValue = original
    ? initialAllDay
      ? shiftEditorDate(dateInput(original.date_to, initialZone, true), -1)
      : dateInput(original.date_to, initialZone, false)
    : initialAllDay
      ? dayKey(initial.endDay ?? initial.day)
      : format(initialEnd, "yyyy-MM-dd'T'HH:mm");
  let startDate = $state(initialStartValue.slice(0, 10));
  let startTime = $state(initialAllDay ? '09:00' : initialStartValue.slice(11));
  let endDate = $state(initialEndValue.slice(0, 10));
  let endTime = $state(initialAllDay ? '10:00' : initialEndValue.slice(11));
  // Names match pkg/ical/special/func.go; only exact presets replace an existing rule.
  const specialDays = [
    ['GOODFRIDAY', 'Good Friday'],
    ['EASTERSUNDAY', 'Easter Sunday'],
    ['EASTERMONDAY', 'Easter Monday'],
    ['ASCENSIONDAY', 'Ascension Day'],
    ['WHITSUNDAY', 'Whit Sunday'],
    ['WHITMONDAY', 'Whit Monday'],
  ];
  const rules = [
    '',
    'RRULE:FREQ=DAILY',
    'RRULE:FREQ=WEEKLY',
    'RRULE:FREQ=MONTHLY',
    'RRULE:FREQ=YEARLY',
    'RRULE:FREQ=HOURLY',
    'RRULE:FREQ=MINUTELY',
    'RRULE:FREQ=SECONDLY',
    ...specialDays.map(([name]) => `FUNC:${name}`),
  ];
  let recurrence = $state(rules.includes(original?.rrule || '') ? original?.rrule || '' : 'custom');
  const specialHoliday = $derived(recurrence.startsWith('FUNC:'));
  const allDay = $derived(allDayChoice);
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

  function changeStart(value: string, time = false) {
    const nextDate = time ? startDate : value;
    const nextTime = time ? value : startTime;
    try {
      if (nextDate && (allDay || nextTime)) {
        const moved = moveEditorEnd(
          allDay ? startDate : `${startDate}T${startTime}`,
          allDay ? endDate : `${endDate}T${endTime}`,
          allDay ? nextDate : `${nextDate}T${nextTime}`,
          zone,
          allDay,
        );
        endDate = moved.slice(0, 10);
        if (!allDay) endTime = moved.slice(11);
      }
      error = '';
    } catch {
      // Incomplete or nonexistent times remain editable; submit validates them.
    }
    startDate = nextDate;
    startTime = nextTime;
  }

  function toggleAllDay(checked: boolean) {
    if (checked) {
      if (/^00:00(?::00)?$/.test(endTime) && endDate > startDate) endDate = shiftEditorDate(endDate, -1);
      if (endDate < startDate) endDate = startDate;
    } else {
      if (/^00:00(?::00)?$/.test(endTime) && endDate) endDate = shiftEditorDate(endDate, 1);
      if (`${endDate}T${endTime}` <= `${startDate}T${startTime}`) {
        startTime = '09:00';
        endTime = '10:00';
        endDate = startDate;
      }
    }
    allDayChoice = checked;
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (busy) return;
    error = '';
    try {
      if (!name.trim()) throw new Error('Give your event a name.');
      if (!startDate || !endDate || (!allDay && (!startTime || !endTime)))
        throw new Error('Choose a start and end date and time.');
      if (
        allDay &&
        /FREQ=(HOURLY|MINUTELY|SECONDLY)/.test(recurrence === 'custom' ? original?.rrule || '' : recurrence)
      )
        throw new Error('Hourly, minutely and secondly repeats require a timed event.');
      if (allDay && endDate < startDate) throw new Error('The end date must be on or after the start date.');
      const unchangedTiming =
        !!original &&
        (timingLocked ||
          (zone === initialZone &&
            allDay === initialAllDay &&
            startDate === initialStartValue.slice(0, 10) &&
            endDate === initialEndValue.slice(0, 10) &&
            (allDay ||
              (startTime === initialStartValue.slice(11) && endTime === initialEndValue.slice(11)))));
      const date_from = unchangedTiming
        ? original!.date_from
        : toInstant(allDay ? startDate : `${startDate}T${startTime}`, zone);
      const date_to = unchangedTiming
        ? original!.date_to
        : toInstant(allDay ? shiftEditorDate(endDate, 1) : `${endDate}T${endTime}`, zone);
      if (Date.parse(date_to) <= Date.parse(date_from)) throw new Error('The end must be after the start.');
      busy = true;
      const edited: CalendarEvent = {
        ...original,
        id: original?.id || '',
        name: name.trim(),
        description: description.trim(),
        event_group: group.trim() || null,
        date_from,
        date_to,
        tz: unchangedTiming ? original!.tz : zone,
        all_day: allDay,
        disabled,
        rrule: recurrence === 'custom' ? original?.rrule || '' : recurrence,
      };
      await saveEvent(
        scope === 'occurrence' && master && occurrence?.recurrence_id
          ? editOccurrence(master, occurrence.recurrence_id, 'edit', edited, original || undefined)
          : editSeries(original, edited),
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
      if (scope === 'occurrence' && master && occurrence?.recurrence_id) {
        await saveEvent(editOccurrence(master, occurrence.recurrence_id, 'cancel'), true, author.trim());
        onsaved('Occurrence cancelled. Restore it from the series exceptions.');
      } else {
        await deleteEvent(original.id);
        onsaved('Event deleted.');
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not delete the event. Please retry.';
    } finally {
      busy = false;
    }
  }

  async function resetException(id: import('./lib/api').CalendarDate) {
    if (!master || busy) return;
    busy = true;
    error = '';
    try {
      await saveEvent(editOccurrence(master, id, 'reset'), true, author.trim());
      onsaved('Exception reset to the series.');
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not reset the exception. Please retry.';
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
      {#if onscope}
        <label for="event-scope">Edit scope</label>
        <select
          id="event-scope"
          value={scope}
          disabled={busy}
          onchange={(e) => onscope?.(e.currentTarget.value as 'occurrence' | 'series')}
        >
          <option value="occurrence">This occurrence</option><option value="series">Entire series</option>
        </select>
        <p class="field-hint">Switching scope discards unsaved edits in this form.</p>
      {/if}
      {#if master?.rrule || master?.recurrence}<div class="notice">
          <Repeat2 size={16} /><span>
            {scope === 'occurrence'
              ? 'Only this occurrence changes. Moving it keeps its original recurrence identity. Calendar group and repeat rule come from the series.'
              : 'Changes apply to the series. Existing occurrence exceptions keep their own details.'}
          </span>
        </div>{/if}
      {#if timingLocked}<p class="field-hint" role="status">
          {unsupportedZone
            ? 'This event uses a custom calendar time zone or imported timezone definitions that the browser cannot verify. Times are shown in UTC; timing is read-only and original projected dates and timezone data are preserved.'
            : 'Series dates, time zone and repeat rule are read-only while exceptions exist (including excluded and additional dates). Reset exceptions before changing timing.'}
        </p>{/if}
      {#if scope === 'occurrence' && occurrence?.is_override && occurrence.recurrence_id}
        <p class="field-hint">Reset saves immediately and closes the editor without saving other edits.</p>
        <button
          type="button"
          class="secondary-button"
          disabled={busy}
          onclick={() => resetException(occurrence!.recurrence_id!)}>Reset this occurrence to series</button
        >
      {/if}
      {#if scope === 'series' && master?.recurrence?.overrides?.length}
        <details class="advanced">
          <summary>Occurrence exceptions ({master.recurrence.overrides.length})</summary>
          <p class="field-hint">
            Reset saves immediately and closes the editor. Other unsaved edits are not saved.
          </p>
          {#each master.recurrence.overrides as exception}
            <p class="field-hint">
              {exception.recurrence_id.value}
              {exception.recurrence_id.tzid || ''}: {exception.cancelled ? 'Cancelled' : 'Overridden'}
            </p>
            <button
              type="button"
              class="secondary-button"
              disabled={busy}
              onclick={() => resetException(exception.recurrence_id)}
              >{exception.cancelled ? 'Restore' : 'Reset'} {exception.recurrence_id.value}</button
            >
          {/each}
        </details>
      {/if}
      {#if scope === 'series' && (master?.recurrence?.exdates?.length || master?.recurrence?.rdates?.length)}
        <p class="field-hint">
          Imported excluded dates ({master?.recurrence?.exdates?.length || 0}) and additional dates ({master
            ?.recurrence?.rdates?.length || 0}) are preserved. Manage these through ICS or the API.
        </p>
      {/if}
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
          <label for="event-group">Calendar group</label><select
            id="event-group"
            bind:value={group}
            disabled={busy || newGroup || scope === 'occurrence'}
            aria-describedby="event-group-help"
          >
            <option value="">No group</option>
            {#each groupOptions as g}<option value={g}>{g}</option>{/each}
          </select>
          <label class="checkbox-label"
            ><input type="checkbox" bind:checked={newGroup} disabled={busy || scope === 'occurrence'} />Enter
            a group name</label
          >
          {#if newGroup}
            <label for="event-new-group">Group name</label><input
              id="event-new-group"
              bind:value={group}
              placeholder="e.g. Team, Holidays"
              disabled={busy}
            />
          {/if}
          <p class="field-hint" id="event-group-help">Choose an existing group or enter a new name.</p>
          {#if groupsLoading}<p class="field-hint" role="status">Loading groups...</p>{/if}
          {#if groupsError}<div class="error-message" role="alert">
              Could not load groups. {groupsError}
              <button
                type="button"
                class="secondary-button"
                disabled={groupsLoading || busy}
                onclick={onretrygroups}>Retry groups</button
              >
            </div>{/if}
        </div>
        <div>
          <label for="event-repeat">Repeat</label><select
            id="event-repeat"
            bind:value={recurrence}
            disabled={busy || timingLocked || scope === 'occurrence'}
            ><option value="">Does not repeat</option><option value="RRULE:FREQ=DAILY">Every day</option
            ><option value="RRULE:FREQ=WEEKLY">Every week</option><option value="RRULE:FREQ=MONTHLY"
              >Every month</option
            ><option value="RRULE:FREQ=YEARLY">Every year</option>
            <option value="RRULE:FREQ=HOURLY" disabled={allDay}>Every hour</option>
            <option value="RRULE:FREQ=MINUTELY" disabled={allDay}>Every minute</option>
            <option value="RRULE:FREQ=SECONDLY" disabled={allDay}>Every second</option>
            <optgroup label="Special holidays (yearly)">
              {#each specialDays as [name, label]}<option value={`FUNC:${name}`}>{label}</option>{/each}
            </optgroup>{#if original?.rrule && !rules.includes(original.rrule)}<option value="custom"
                >Keep existing rule</option
              >{/if}</select
          >
        </div>
      </div>
      {#if recurrence === 'custom'}<p class="field-hint rule-preview">{original?.rrule}</p>{/if}
      {#if specialHoliday}<p class="field-hint" id="event-special-help">
          Repeats on the selected holiday each year, keeping the event's timing and duration.
        </p>{/if}
      <label class="checkbox-label"
        ><input
          type="checkbox"
          checked={allDay}
          onchange={(e) => toggleAllDay(e.currentTarget.checked)}
          disabled={busy || timingLocked || scope === 'occurrence'}
          aria-describedby={specialHoliday ? 'event-special-help' : undefined}
        />All-day event</label
      >
      <div class="form-row">
        <div>
          <label for="event-start">Start date</label><input
            id="event-start"
            type="date"
            value={startDate}
            onchange={(e) => changeStart(e.currentTarget.value)}
            required
            disabled={busy || timingLocked}
            aria-describedby={specialHoliday ? 'event-special-help' : undefined}
          />
          {#if !allDay}
            <label for="event-start-time">Start time</label><input
              id="event-start-time"
              type="time"
              step="1"
              value={startTime}
              onchange={(e) => changeStart(e.currentTarget.value, true)}
              aria-describedby="event-zone-help"
              required
              disabled={busy || timingLocked}
            />
          {/if}
        </div>
        <div>
          <label for="event-end">End date</label><input
            id="event-end"
            type="date"
            value={endDate}
            onchange={(e) => (endDate = e.currentTarget.value)}
            min={startDate}
            aria-describedby={specialHoliday
              ? 'event-special-help'
              : allDay
                ? 'event-all-day-help'
                : undefined}
            required
            disabled={busy || timingLocked}
          />
          {#if !allDay}
            <label for="event-end-time">End time</label><input
              id="event-end-time"
              type="time"
              step="1"
              bind:value={endTime}
              aria-describedby="event-zone-help"
              required
              disabled={busy || timingLocked}
            />
          {/if}
        </div>
      </div>
      {#if allDay}<p class="field-hint" id="event-all-day-help">
          Includes the end date. For a one-day event, use the same start and end date.
        </p>{/if}
      <p class="field-hint">Changing the start moves the end to keep the same duration.</p>
      {#if original?.recurrence?.duration && !timingLocked && scope === 'series'}
        <p class="field-hint">
          This import uses DURATION ({original.recurrence.duration}). Editing timing replaces it with an
          explicit end; details-only edits preserve it.
        </p>
      {/if}
      <label for="event-zone">Time zone</label><select
        id="event-zone"
        bind:value={zone}
        required
        aria-describedby="event-zone-help"
        disabled={busy || timingLocked}
      >
        {#each zones as timezone}
          <option value={timezone}
            >{timezone === 'UTC' ? 'UTC (Coordinated Universal Time)' : timezone}{timezone === localZone
              ? ' (local)'
              : ''}</option
          >
        {/each}
      </select>
      <p class="field-hint" id="event-zone-help">
        {#if allDay}Dates use the selected time zone.{:else}Times use the selected time zone. Changing it
          keeps the entered times.{/if}
        Calendar times are displayed in {localZone} (local).
      </p>
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
          <p>
            {scope === 'occurrence'
              ? 'Cancel this occurrence? You can restore it in the series exceptions.'
              : `Delete ${original?.rrule || original?.recurrence ? 'this entire series' : 'this event'}? This cannot be undone.`}
          </p>
          <div class="flex gap-2">
            <button
              type="button"
              class="secondary-button"
              disabled={busy}
              onclick={() => (confirmingDelete = false)}>Keep event</button
            ><button type="button" class="danger-button" disabled={busy} onclick={remove}
              >{busy
                ? 'Saving…'
                : scope === 'occurrence'
                  ? 'Confirm cancellation'
                  : 'Delete permanently'}</button
            >
          </div>
        </div>
      {:else}
        {#if original}<button
            type="button"
            class="delete-button"
            disabled={busy}
            onclick={() => (confirmingDelete = true)}
            ><Trash2 size={16} />{scope === 'occurrence' ? 'Cancel occurrence' : 'Delete'}</button
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
