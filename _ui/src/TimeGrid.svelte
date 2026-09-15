<script lang="ts">
  import { addDays, format, isSameDay } from 'date-fns';
  import { onMount } from 'svelte';
  import type { CalendarEvent } from './lib/api';
  import { dragEvent } from './lib/dragEvent';
  import { resizeEvent } from './lib/resizeEvent';
  import { resizeAllDay } from './lib/resizeAllDay';
  import { eventLanes } from './lib/eventLanes';
  import { colorFor, groupName, localZone, occursOn } from './lib/calendar';

  let {
    start,
    dayCount = 7,
    events,
    selected,
    catalogLoading,
    onselect,
    onopen,
    oncreate,
    onmove,
    onresize,
    oncreateAllDay,
  }: {
    start: Date;
    dayCount?: 1 | 7;
    events: CalendarEvent[];
    selected: Date;
    catalogLoading: boolean;
    onselect: (day: Date) => void;
    onopen: (event: CalendarEvent) => void;
    oncreate: (start: Date, end: Date) => void;
    onmove: (event: CalendarEvent, target: Date) => void;
    onresize: (event: CalendarEvent, start: Date, end: Date) => void;
    oncreateAllDay: (start: Date, end: Date) => void;
  } = $props();
  let scroll: HTMLDivElement;
  let selection = $state<{ day: number; anchor: number; end: number } | null>(null);
  let allDaySelection = $state<{ pointerId: number; anchor: number; end: number } | null>(null);
  function cancelSelection() { selection = null; allDaySelection = null; }
  let activeSlot = $state({ day: 0, slot: 18 });
  const days = $derived(Array.from({ length: dayCount }, (_, i) => addDays(start, i)));
  const slots = Array.from({ length: 48 }, (_, i) => i);
  const allDayLanes = $derived(eventLanes(events.filter(event => event.all_day), days));
  const timed = $derived(
    days.map((day) => {
      const items = events
        .filter((e) => !e.all_day && occursOn(e, day))
        .map((event) => {
          const from = new Date(event.date_from);
          const to = new Date(event.date_to);
          return {
            event,
            from: isSameDay(from, day) ? from.getHours() * 60 + from.getMinutes() : 0,
            to: isSameDay(to, day) ? to.getHours() * 60 + to.getMinutes() : 1440,
            lane: 0,
            lanes: 1,
          };
        })
        .sort((a, b) => a.from - b.from || b.to - a.to);
      // Each connected overlap group shares columns, so simultaneous events stay clickable.
      let group: typeof items = [];
      let groupEnd = -1;
      let ends: number[] = [];
      for (const item of items) {
        if (item.from >= groupEnd) {
          for (const previous of group) previous.lanes = ends.length;
          group = [];
          ends = [];
        }
        let lane = ends.findIndex((end) => end <= item.from);
        if (lane < 0) lane = ends.length;
        ends[lane] = item.to;
        item.lane = lane;
        group.push(item);
        groupEnd = Math.max(...ends);
      }
      for (const item of group) item.lanes = ends.length;
      return items;
    }),
  );

  onMount(() => {
    scroll.scrollTop = 8 * 56;
  });

  function create(day: number, from: number, to = from) {
    const first = new Date(days[day]);
    first.setHours(0, Math.min(from, to) * 30, 0, 0);
    const last = new Date(days[day]);
    last.setHours(0, (Math.max(from, to) + 1) * 30, 0, 0);
    oncreate(first, last);
  }

  function move(event: PointerEvent) {
    if (allDaySelection && event.pointerId === allDaySelection.pointerId) {
      const cell = document.elementFromPoint(event.clientX, event.clientY)?.closest<HTMLElement>('[data-all-day-index]');
      if (cell && scroll.contains(cell)) allDaySelection.end = Number(cell.dataset.allDayIndex);
      return;
    }
    if (!selection) return;
    const target = document
      .elementFromPoint(event.clientX, event.clientY)
      ?.closest<HTMLElement>('.week-column');
    if (target && Number(target.dataset.day) === selection.day)
      selection.end = Math.max(
        0,
        Math.min(47, Math.floor((event.clientY - target.getBoundingClientRect().top) / 28)),
      );
  }

  function finish(event: PointerEvent) {
    if (allDaySelection) {
      if (event.pointerId !== allDaySelection.pointerId) return;
      const { anchor, end } = allDaySelection;
      allDaySelection = null;
      const cell = document.elementFromPoint(event.clientX, event.clientY)?.closest('[data-all-day-index]');
      if (cell && scroll.contains(cell)) oncreateAllDay(days[Math.min(anchor, end)], days[Math.max(anchor, end)]);
      return;
    }
    if (!selection) return;
    const { day, anchor, end } = selection;
    selection = null;
    create(day, anchor, end);
  }
</script>

<svelte:window
  onpointermove={move}
  onpointerup={finish}
  onpointercancel={cancelSelection}
  onblur={cancelSelection}
  onkeydown={(event) => {
    if (event.key === 'Escape') cancelSelection();
  }}
/>

<div class="week-help">
  <span>{localZone}</span><span class="week-desktop-hint"
    >Drag events to move · Drag top/bottom edges to resize · Drag across All day to create</span
  >
  <span class="week-touch-hint">Tap a time; swipe to browse the {dayCount === 1 ? 'day' : 'week'}</span>
</div>
<div class="week-scroll" bind:this={scroll}>
  <div class="week-canvas" class:single-day={dayCount === 1} style={`--day-count: ${dayCount}`}>
    <div class="week-header">
      <span class="week-corner">Time</span>
      {#each days as day}<button
          class:selected={isSameDay(day, selected)}
          aria-label={`Select ${format(day, 'EEEE, MMMM d, yyyy')}`}
          aria-pressed={isSameDay(day, selected)}
          onclick={() => onselect(day)}
          ><span>{format(day, 'EEE')}</span><strong>{format(day, 'd')}</strong></button
        >{/each}
    </div>
    <div class="week-all-day">
      <span>All day</span>
      {#each days as day, index}<div class="all-day-drop" data-drop-date={format(day, 'yyyy-MM-dd')} data-all-day-index={index}
          class:all-day-selected={allDaySelection !== null && index >= Math.min(allDaySelection.anchor, allDaySelection.end) && index <= Math.max(allDaySelection.anchor, allDaySelection.end)}>
          <button class="all-day-create" aria-label={`Create all-day event on ${format(day, 'MMMM d, yyyy')}`}
            onpointerdown={(event) => {
              if (event.button !== 0 || !event.isPrimary || event.pointerType === 'touch') return;
              allDaySelection = { pointerId: event.pointerId, anchor: index, end: index };
            }}
            onclick={(event) => {
              if (event.detail === 0 || (event instanceof PointerEvent && event.pointerType === 'touch')) oncreateAllDay(day, day);
            }}></button>
          {#each allDayLanes[index] as event}
            {#if event}
            <button
              use:resizeAllDay={{ event, day, enabled: !catalogLoading && !event.subscription_id, onresize }}
              use:dragEvent={{ event, day, enabled: !catalogLoading && !event.subscription_id, onmove }}
              class={`event-chip all-day-segment event-color-${colorFor(groupName(event))}`}
              class:joins-left={index > 0 && occursOn(event, addDays(day, -1))}
              class:joins-right={index < days.length - 1 && occursOn(event, addDays(day, 1))}
              disabled={catalogLoading}
              onclick={() => onopen(event)}
              title={event.name}>
              {#if index === 0 && occursOn(event, addDays(day, -1))}<span aria-label="Continues from previous days">‹</span>{/if}
              <span class="event-chip-name">{event.name || 'Untitled event'}</span>
              {#if index === days.length - 1 && occursOn(event, addDays(day, 1))}<span aria-label="Continues on following days">›</span>{/if}
            </button
            >
            {:else}<span class="event-lane-spacer" aria-hidden="true"></span>{/if}
          {/each}
        </div>{/each}
    </div>
    <div class="week-hours">
      <div class="week-times">
        {#each slots.filter((slot) => slot % 2 === 0) as slot}<time
            >{String(slot / 2).padStart(2, '0')}:00</time
          >{/each}
      </div>
      {#each days as day, index}
        <div class="week-column" data-day={index} data-drop-date={format(day, 'yyyy-MM-dd')}>
          {#each slots as slot}<button
              class="week-slot"
              class:hour-start={slot % 2 === 0}
              class:range-selected={selection?.day === index &&
                slot >= Math.min(selection.anchor, selection.end) &&
                slot <= Math.max(selection.anchor, selection.end)}
              data-week-slot={slot}
              data-day={index}
              tabindex={activeSlot.day === index && activeSlot.slot === slot ? 0 : -1}
              onfocus={() => (activeSlot = { day: index, slot })}
              onkeydown={(event) => {
                const offset = {
                  ArrowUp: [-1, 0],
                  ArrowDown: [1, 0],
                  ArrowLeft: [0, -1],
                  ArrowRight: [0, 1],
                }[event.key];
                if (!offset) return;
                event.preventDefault();
                const nextSlot = Math.max(0, Math.min(47, slot + offset[0]));
                const nextDay = Math.max(0, Math.min(days.length - 1, index + offset[1]));
                scroll
                  .querySelector<HTMLButtonElement>(`[data-week-slot="${nextSlot}"][data-day="${nextDay}"]`)
                  ?.focus();
              }}
              aria-label={`Create event on ${format(day, 'MMMM d, yyyy')} at ${String(Math.floor(slot / 2)).padStart(2, '0')}:${slot % 2 ? '30' : '00'}`}
              onpointerdown={(event) => {
                if (event.button !== 0 || event.pointerType === 'touch') return;
                selection = { day: index, anchor: slot, end: slot };
              }}
              onclick={(event) => {
                // Wait for the touch click so it cannot activate a control in the new dialog.
                if (event.detail === 0 || (event instanceof PointerEvent && event.pointerType === 'touch'))
                  create(index, slot);
              }}><span>{String(Math.floor(slot / 2)).padStart(2, '0')}:{slot % 2 ? '30' : '00'}</span></button
            >{/each}
          {#each timed[index] as item}
            <button
              use:resizeEvent={{ event: item.event, day, enabled: !catalogLoading && !item.event.subscription_id, onresize }}
              use:dragEvent={{
                event: item.event,
                day,
                enabled: !catalogLoading && !item.event.subscription_id,
                onmove,
              }}
              class={`week-event event-color-${colorFor(groupName(item.event))}`}
              class:disabled-event={item.event.disabled}
              style:top={`${(item.from / 1440) * 100}%`}
              style:height={`${(Math.max(item.to - item.from, 15) / 1440) * 100}%`}
              style:left={`calc(${(item.lane / item.lanes) * 100}% + 2px)`}
              style:width={`calc(${100 / item.lanes}% - 4px)`}
              disabled={catalogLoading}
              onclick={() => onopen(item.event)}
              title={`${item.event.name} · ${format(new Date(item.event.date_from), 'HH:mm')} - ${format(new Date(item.event.date_to), 'HH:mm')}`}
              ><strong>{item.event.name || 'Untitled event'}</strong><span
                >{format(new Date(item.event.date_from), 'HH:mm')} - {format(
                  new Date(item.event.date_to),
                  'HH:mm',
                )}</span
              ></button
            >
          {/each}
        </div>
      {/each}
    </div>
  </div>
</div>
