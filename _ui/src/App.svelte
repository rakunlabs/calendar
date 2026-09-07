<script lang="ts">
  import { tick, onDestroy } from 'svelte';
  import {
    CalendarDays,
    ChevronLeft,
    ChevronRight,
    Plus,
    Search,
    ArrowUpRight,
    Clock3,
    Repeat2,
    RefreshCw,
    AlertCircle,
    CheckCircle2,
    X,
    SlidersHorizontal,
  } from '@lucide/svelte';
  import { addDays, addMonths, addYears, format, isSameDay, isSameMonth, startOfMonth } from 'date-fns';
  import { getEvents, getOccurrences, type CalendarEvent } from './lib/api';
  import {
    colorFor,
    dayKey,
    groupName,
    localZone,
    monthDays,
    occursOn,
    timeLabel,
    viewRange,
    weekdays,
    type View,
  } from './lib/calendar';
  import EventEditor from './EventEditor.svelte';

  const today = new Date();
  let selected = $state(new Date());
  let focus = $state(new Date());
  let view = $state<View>('month');
  let search = $state('');
  let hiddenGroups = $state<string[]>([]);
  let showDisabled = $state(false);
  let filtersOpen = $state(false);
  let templates = $state<CalendarEvent[]>([]);
  let occurrences = $state<CalendarEvent[]>([]);
  let catalogLoading = $state(true);
  let loading = $state(true);
  let catalogError = $state('');
  let rangeError = $state('');
  let revision = $state(0);
  let editor = $state<{ event: CalendarEvent | null; day: Date; hour?: number } | null>(null);
  let toast = $state('');
  let toastTimer: ReturnType<typeof setTimeout>;
  onDestroy(() => clearTimeout(toastTimer));

  async function openFilters() {
    filtersOpen = true;
    await tick();
    document.querySelector<HTMLButtonElement>('.sidebar-close')?.focus();
  }
  async function closeFilters() {
    filtersOpen = false;
    await tick();
    document.querySelector<HTMLButtonElement>('.mobile-filter')?.focus();
  }

  const groups = $derived([...new Set(templates.map(groupName))].sort());
  const days = $derived(monthDays(focus));
  const range = $derived(viewRange(focus, view));
  const filtered = $derived(
    occurrences.filter(
      (event) =>
        (showDisabled || !event.disabled) &&
        !hiddenGroups.includes(groupName(event)) &&
        `${event.name} ${event.description} ${event.event_group || ''}`
          .toLowerCase()
          .includes(search.toLowerCase().trim()),
    ),
  );
  const selectedEvents = $derived(filtered.filter((event) => occursOn(event, selected)));
  const daysWithEvents = $derived.by(() => {
    const map = new Map<string, CalendarEvent[]>();
    for (let day = range[0]; day < range[1]; day = addDays(day, 1))
      map.set(
        dayKey(day),
        filtered.filter((e) => occursOn(e, day)),
      );
    return map;
  });
  const heading = $derived(
    format(focus, view === 'year' ? 'yyyy' : view === 'day' ? 'MMMM d, yyyy' : 'MMMM yyyy'),
  );

  $effect(() => {
    revision;
    const controller = new AbortController();
    catalogLoading = true;
    catalogError = '';
    getEvents(controller.signal)
      .then((data) => (templates = data))
      .catch((e) => {
        if (!controller.signal.aborted) catalogError = e.message;
      })
      .finally(() => {
        if (!controller.signal.aborted) catalogLoading = false;
      });
    return () => controller.abort();
  });

  $effect(() => {
    revision;
    const [from, to] = range;
    // Include a day on either side for all-day events in distant time zones.
    const controller = new AbortController();
    loading = true;
    rangeError = '';
    occurrences = [];
    getOccurrences(addDays(from, -1), addDays(to, 1), controller.signal)
      .then((data) => (occurrences = data))
      .catch((e) => {
        if (!controller.signal.aborted) rangeError = e.message;
      })
      .finally(() => {
        if (!controller.signal.aborted) loading = false;
      });
    return () => controller.abort();
  });

  function navigate(direction: number) {
    focus =
      view === 'year'
        ? addYears(focus, direction)
        : view === 'month'
          ? addMonths(startOfMonth(focus), direction)
          : addDays(focus, direction);
    selected = focus;
  }
  function selectDay(day: Date) {
    selected = day;
    if (!isSameMonth(day, focus) || view === 'day') focus = day;
  }
  function chooseView(next: View) {
    view = next;
    focus = selected;
  }
  function goToday() {
    focus = new Date();
    selected = focus;
  }
  function toggleGroup(group: string) {
    hiddenGroups = hiddenGroups.includes(group)
      ? hiddenGroups.filter((g) => g !== group)
      : [...hiddenGroups, group];
  }
  function openEvent(event: CalendarEvent) {
    const original = templates.find((e) => e.id === event.id);
    if (!original) {
      notify('Event details are still loading. Please retry in a moment.');
      return;
    }
    editor = { event: original, day: selected };
  }
  function notify(message: string) {
    toast = message;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => (toast = ''), 5000);
  }
  function saved(message: string) {
    editor = null;
    revision++;
    notify(message);
  }
</script>

<svelte:head><title>{heading} · Calendar</title></svelte:head>
<svelte:window
  onkeydown={(e) => {
    if (e.key === 'Escape' && filtersOpen && !editor) closeFilters();
  }}
  onresize={() => {
    if (window.innerWidth > 700) filtersOpen = false;
  }}
/>

<div class="app-shell">
  {#if filtersOpen}<button class="sidebar-backdrop" aria-label="Close calendar filters" onclick={closeFilters}
    ></button>{/if}
  <aside class:mobile-open={filtersOpen} class="sidebar" aria-label="Calendar navigation">
    <button class="icon-button sidebar-close" aria-label="Close filters" onclick={closeFilters}
      ><X size={19} /></button
    >
    <a class="brand" href={import.meta.env.BASE_URL}
      ><span class="brand-mark"><CalendarDays size={22} strokeWidth={1.8} /></span><span
        >calendar<span class="brand-period">.</span></span
      ></a
    >
    <div class="workspace-label"><span class="status-dot"></span> Your calendar workspace</div>
    <button class="primary-button new-event-sidebar" onclick={() => (editor = { event: null, day: selected })}
      ><Plus size={18} />New event</button
    >
    <div class="mini-calendar">
      <div class="mini-title">
        <strong>{format(focus, 'MMMM yyyy')}</strong>
        <div class="flex">
          <button
            class="icon-button small"
            aria-label="Previous month in mini calendar"
            onclick={() => {
              focus = addMonths(startOfMonth(focus), -1);
              selected = focus;
            }}><ChevronLeft size={15} /></button
          ><button
            class="icon-button small"
            aria-label="Next month in mini calendar"
            onclick={() => {
              focus = addMonths(startOfMonth(focus), 1);
              selected = focus;
            }}><ChevronRight size={15} /></button
          >
        </div>
      </div>
      <div class="mini-grid">
        {#each weekdays as day}<span class="mini-weekday">{day.slice(0, 1)}</span>{/each}
        {#each monthDays(focus) as day}<button
            class:muted={!isSameMonth(day, focus)}
            class:mini-selected={isSameDay(day, selected)}
            class:mini-today={isSameDay(day, today)}
            aria-label={format(day, 'EEEE, MMMM d, yyyy')}
            aria-pressed={isSameDay(day, selected)}
            onclick={() => selectDay(day)}>{day.getDate()}</button
          >{/each}
      </div>
    </div>
    <section class="calendar-groups" aria-labelledby="groups-heading">
      <div class="section-heading">
        <h2 id="groups-heading">My calendars</h2>
        <span>{groups.length}</span>
      </div>
      {#if catalogLoading}<p class="sidebar-hint">Loading calendars…</p>{:else if groups.length === 0}<p
          class="sidebar-hint"
        >
          Groups appear when you add your first event.
        </p>{/if}
      {#each groups as group}<label class="group-filter"
          ><input
            type="checkbox"
            checked={!hiddenGroups.includes(group)}
            onchange={() => toggleGroup(group)}
            style={`accent-color: var(--event-${colorFor(group)})`}
          /><span>{group}</span><span class="group-count"
            >{templates.filter((e) => groupName(e) === group).length}</span
          ></label
        >{/each}
      <label class="group-filter disabled-filter"
        ><input type="checkbox" bind:checked={showDisabled} /><span>Show disabled events</span></label
      >
    </section>
    <div class="sidebar-bottom">
      <div class="zone-label"><Clock3 size={15} /><span>{localZone.replaceAll('_', ' ')}</span></div>
      <p>One place for all your plans.</p>
      <a href={`${import.meta.env.BASE_URL}swagger/index.html`} target="_blank" rel="noreferrer"
        >API documentation <ArrowUpRight size={14} /></a
      >
    </div>
  </aside>

  <main inert={filtersOpen}>
    <header class="topbar">
      <div class="breadcrumb">
        <CalendarDays size={17} /><span>Workspace</span><span class="crumb-separator">/</span><strong
          >Calendar</strong
        >
      </div>
      <div class="topbar-right">
        <span class="today-label">{format(today, 'EEE, d MMM')}</span><span
          class="workspace-avatar"
          aria-label="Calendar workspace">C</span
        >
      </div>
    </header>
    <div class="page-content">
      <div class="page-heading">
        <div>
          <h1>Your time, in view.</h1>
          <p>A little perspective on everything coming up.</p>
        </div>
        <button
          class="primary-button desktop-create"
          onclick={() => (editor = { event: null, day: selected })}><Plus size={17} />Create event</button
        >
      </div>
      <div class="calendar-toolbar">
        <div class="period-controls">
          <button class="secondary-button today-button" onclick={goToday}>Today</button>
          <div class="arrow-pair">
            <button class="icon-button" aria-label={`Previous ${view}`} onclick={() => navigate(-1)}
              ><ChevronLeft size={19} /></button
            ><button class="icon-button" aria-label={`Next ${view}`} onclick={() => navigate(1)}
              ><ChevronRight size={19} /></button
            >
          </div>
          <h2 aria-live="polite">{heading}</h2>
        </div>
        <div class="toolbar-end">
          <button
            class="icon-button mobile-filter"
            aria-label="Toggle calendar filters"
            aria-expanded={filtersOpen}
            onclick={openFilters}><SlidersHorizontal size={18} /></button
          ><button
            class="icon-button"
            aria-label="Refresh calendar"
            disabled={loading || catalogLoading}
            onclick={() => revision++}><RefreshCw size={17} class={loading ? 'spin' : ''} /></button
          >
          <div class="view-switch" aria-label="Calendar view">
            {#each ['month', 'day', 'year'] as mode}<button
                class:active={view === mode}
                aria-pressed={view === mode}
                onclick={() => chooseView(mode as View)}>{mode[0].toUpperCase() + mode.slice(1)}</button
              >{/each}
          </div>
        </div>
      </div>
      <div class="search-line">
        <label class="search-field"
          ><Search size={16} /><input
            aria-label="Search events"
            bind:value={search}
            placeholder="Search events, notes, or groups"
          />{#if search}<button
              class="icon-button small"
              aria-label="Clear search"
              onclick={() => (search = '')}><X size={14} /></button
            >{/if}</label
        ><span
          >{loading
            ? 'Updating calendar…'
            : `${filtered.length} ${filtered.length === 1 ? 'occurrence' : 'occurrences'} in view`}</span
        >
      </div>
      {#if catalogError || rangeError}<div class="error-banner" role="alert">
          <AlertCircle size={19} />
          <div>
            <strong>We couldn't load your calendar.</strong>
            <p>{catalogError || rangeError}</p>
          </div>
          <button class="secondary-button" onclick={() => revision++}>Try again</button>
        </div>{/if}
      <div class="calendar-layout" aria-busy={loading}>
        <section class="calendar-surface" aria-label={`${view} calendar`}>
          {#if view === 'month'}
            <div class="weekdays">
              {#each weekdays as name}<span>{name}</span>{/each}
            </div>
            <div class="month-grid">
              {#each days as day}
                {@const dayEvents = daysWithEvents.get(dayKey(day)) || []}
                <div
                  class="month-cell"
                  class:other-month={!isSameMonth(day, focus)}
                  class:selected-day={isSameDay(day, selected)}
                  class:weekend={day.getDay() === 0 || day.getDay() === 6}
                >
                  <button
                    class="day-select"
                    aria-label={`Select ${format(day, 'EEEE, MMMM d, yyyy')}, ${dayEvents.length} events`}
                    aria-pressed={isSameDay(day, selected)}
                    onclick={() => selectDay(day)}
                    ><span class:today-number={isSameDay(day, today)}>{day.getDate()}</span
                    >{#if isSameDay(day, today)}<span class="today-word">Today</span>{/if}</button
                  >
                  <div class="cell-events">
                    {#each dayEvents.slice(0, 3) as event}<button
                        class={`event-chip event-color-${colorFor(groupName(event))}`}
                        class:disabled-event={event.disabled}
                        onclick={() => openEvent(event)}
                        disabled={catalogLoading}
                        title={`${event.name} · ${timeLabel(event)}`}
                        ><span class="event-dot"></span><span class="event-chip-name"
                          >{event.name || 'Untitled event'}</span
                        >{#if !event.all_day}<time>{timeLabel(event)}</time>{/if}</button
                      >{/each}
                    {#if dayEvents.length > 3}<button class="more-events" onclick={() => selectDay(day)}
                        >+{dayEvents.length - 3} more</button
                      >{/if}
                  </div>
                  <div class="mobile-dots" aria-hidden="true">
                    {#each dayEvents.slice(0, 3) as event}<span
                        style={`background:var(--event-${colorFor(groupName(event))})`}
                      ></span>{/each}
                  </div>
                </div>
              {/each}
            </div>
          {:else if view === 'year'}
            <div class="year-grid">
              {#each Array.from({ length: 12 }, (_, i) => new Date(focus.getFullYear(), i, 1)) as month}<section
                  class="year-month"
                >
                  <button
                    class="year-month-title"
                    onclick={() => {
                      focus = month;
                      selected = month;
                      view = 'month';
                    }}>{format(month, 'MMMM')}<ArrowUpRight size={13} /></button
                  >
                  <div class="mini-grid">
                    {#each weekdays as day}<span class="mini-weekday">{day.slice(0, 1)}</span
                      >{/each}{#each monthDays(month, false) as day}{@const count = (
                        daysWithEvents.get(dayKey(day)) || []
                      ).length}<button
                        class:outside={!isSameMonth(day, month)}
                        class:has-events={count > 0}
                        class:mini-today={isSameDay(day, today)}
                        aria-label={`${format(day, 'MMMM d')}, ${count} events`}
                        disabled={!isSameMonth(day, month)}
                        onclick={() => {
                          selected = day;
                          focus = day;
                          view = 'day';
                        }}
                        >{day.getDate()}{#if count}<i></i>{/if}</button
                      >{/each}
                  </div>
                </section>{/each}
            </div>
          {:else}
            <div class="day-view">
              <div class="day-view-title">
                <span class="big-date">{format(focus, 'dd')}</span>
                <div>
                  <h3>{format(focus, 'EEEE')}</h3>
                  <p>{format(focus, 'MMMM yyyy')} · {localZone}</p>
                </div>
              </div>
              {#if selectedEvents.some((e) => e.all_day)}<div class="all-day-row">
                  <span>ALL DAY</span>
                  <div>
                    {#each selectedEvents.filter((e) => e.all_day) as event}<button
                        class={`event-chip event-color-${colorFor(groupName(event))}`}
                        disabled={catalogLoading}
                        onclick={() => openEvent(event)}><span class="event-dot"></span>{event.name}</button
                      >{/each}
                  </div>
                </div>{/if}
              {#each Array.from({ length: 24 }, (_, i) => i) as hour}<div class="hour-row">
                  <time>{String(hour).padStart(2, '0')}:00</time>
                  <div>
                    {#each selectedEvents.filter((e) => !e.all_day && (isSameDay(new Date(e.date_from), focus) ? new Date(e.date_from).getHours() : 0) === hour) as event}<button
                        class={`day-event event-color-${colorFor(groupName(event))}`}
                        onclick={() => openEvent(event)}
                        disabled={catalogLoading}
                        ><strong>{event.name}</strong><span
                          >{format(new Date(event.date_from), 'HH:mm')} – {format(
                            new Date(event.date_to),
                            'HH:mm',
                          )} · {groupName(event)}</span
                        ></button
                      >{/each}<button
                      class="hour-add"
                      aria-label={`Add event at ${hour}:00`}
                      onclick={() => (editor = { event: null, day: selected, hour })}
                      ><Plus size={14} /></button
                    >
                  </div>
                </div>{/each}
            </div>
          {/if}
          {#if loading}<div class="loading-strip" role="status">
              <RefreshCw size={14} class="spin" />Loading events…
            </div>{/if}
        </section>

        <aside class="agenda" aria-label="Selected day agenda">
          <div class="agenda-heading">
            <div>
              <h2>{format(selected, 'EEEE')}</h2>
              <p>{format(selected, 'MMMM d, yyyy')}</p>
            </div>
            <span class="agenda-date">{format(selected, 'dd')}</span>
          </div>
          <div class="agenda-divider">
            <span>{isSameDay(selected, today) ? "TODAY'S SCHEDULE" : 'DAY SCHEDULE'}</span><span
              >{selectedEvents.length}</span
            >
          </div>
          {#if loading}<p class="agenda-loading">
              Finding your plans…
            </p>{:else if selectedEvents.length === 0}<div class="empty-agenda">
              <div class="empty-calendar"><CalendarDays size={30} strokeWidth={1.3} /></div>
              <h3>{search || hiddenGroups.length ? 'Nothing matches here.' : 'A little breathing room.'}</h3>
              <p>
                {search || hiddenGroups.length
                  ? 'Try another day or adjust your filters.'
                  : 'No events on this day. Keep it open, or make a new plan.'}
              </p>
              <button class="text-button" onclick={() => (editor = { event: null, day: selected })}
                ><Plus size={15} />Add an event</button
              >
            </div>{:else}<div class="agenda-events">
              {#each selectedEvents as event}<button
                  class="agenda-event"
                  disabled={catalogLoading}
                  onclick={() => openEvent(event)}
                  ><div class="agenda-event-meta">
                    <span class={`group-pill event-color-${colorFor(groupName(event))}`}
                      ><span class="event-dot"></span>{groupName(event)}</span
                    >{#if event.rrule}<Repeat2 size={14} aria-label="Recurring event" />{/if}
                  </div>
                  <h3>{event.name || 'Untitled event'}</h3>
                  <div class="agenda-time">
                    <Clock3 size={13} /><span
                      >{event.all_day
                        ? 'All day'
                        : `${format(new Date(event.date_from), 'HH:mm')} – ${format(new Date(event.date_to), 'HH:mm')}`}</span
                    >{#if event.disabled}<span class="disabled-label">Disabled</span>{/if}
                  </div>
                  {#if event.description}<p>{event.description}</p>{/if}<span class="edit-event"
                    >View & edit <ArrowUpRight size={13} /></span
                  ></button
                >{/each}
            </div>{/if}
          <button class="agenda-add" onclick={() => (editor = { event: null, day: selected })}
            ><Plus size={17} />Plan something for this day</button
          >
        </aside>
      </div>
      <footer class="page-footer">
        <span
          ><span class="status-dot"></span>{catalogError || rangeError
            ? 'Connection needs attention'
            : 'Connected to Calendar API'}</span
        ><span>Times shown in {localZone} · All-day dates use the event time zone</span>
      </footer>
    </div>
    <button
      class="mobile-create primary-button"
      onclick={() => (editor = { event: null, day: selected })}
      aria-label="Create event"><Plus size={22} /></button
    >
  </main>
</div>
{#if toast}<div class="toast" role="status">
    <CheckCircle2 size={18} /><span>{toast}</span><button
      class="icon-button small"
      aria-label="Dismiss notification"
      onclick={() => (toast = '')}><X size={15} /></button
    >
  </div>{/if}
{#if editor}<EventEditor
    event={editor.event}
    day={editor.day}
    hour={editor.hour}
    {groups}
    onclose={() => (editor = null)}
    onsaved={saved}
  />{/if}
