import { addDays, format, isSameDay } from 'date-fns';
import type { CalendarEvent } from './api';
import { dateInput } from './calendar';

type Options = {
  event: CalendarEvent;
  day: Date;
  enabled: boolean;
  onresize: (event: CalendarEvent, start: Date, end: Date) => void;
};

export function resizeAllDay(node: HTMLButtonElement, options: Options) {
  const handles = ['left', 'right'].map(side => {
    const handle = document.createElement('span');
    handle.className = `event-resize-handle all-day-resize-${side}`;
    handle.title = `Drag to change ${side === 'left' ? 'start' : 'end'} date`;
    handle.setAttribute('aria-hidden', 'true');
    node.append(handle);
    return handle;
  });
  let cleanup = () => {};
  let suppressClick = false;
  let timer: ReturnType<typeof setTimeout>;
  function dates() {
    return [options.event.date_from, options.event.date_to].map(value =>
      new Date(`${dateInput(value, options.event.tz || 'UTC', true)}T00:00:00`));
  }
  function refresh() {
    const [start, end] = dates();
    handles[0].hidden = !options.enabled || !options.event.all_day || !isSameDay(start, options.day);
    handles[1].hidden = !options.enabled || !options.event.all_day || !isSameDay(addDays(end, -1), options.day);
  }
  refresh();
  function down(e: PointerEvent) {
    if (!options.enabled || !options.event.all_day || e.button !== 0 || !e.isPrimary || e.pointerType === 'touch') return;
    e.preventDefault();
    e.stopPropagation();
    cleanup();
    const original = options;
    const atStart = e.currentTarget === handles[0];
    const [start, end] = dates();
    const root = node.closest<HTMLElement>('.calendar-surface')!;
    const cell = node.closest<HTMLElement>('[data-drop-date]')!;
    const cells = [...root.querySelectorAll<HTMLElement>('.month-cell, .all-day-drop')];
    const offset = node.getBoundingClientRect().top - cell.getBoundingClientRect().top;
    const height = node.getBoundingClientRect().height;
    const color = getComputedStyle(node);
    const background = color.backgroundColor;
    const ink = color.color;
    const previews: HTMLElement[] = [];
    const status = document.createElement('div');
    status.className = 'event-drag-status';
    status.setAttribute('role', 'status');
    let targetStart = start;
    let targetEnd = end;
    let dragging = false;
    let valid = false;
    function clearPreviews() { previews.splice(0).forEach(preview => preview.remove()); }
    function move(p: PointerEvent) {
      if (p.pointerId !== e.pointerId) return;
      if (!dragging && Math.hypot(p.clientX - e.clientX, p.clientY - e.clientY) < 5) return;
      dragging = true;
      clearPreviews();
      const hit = document.elementFromPoint(p.clientX, p.clientY)?.closest<HTMLElement>('[data-drop-date]');
      valid = !!hit && cells.includes(hit);
      if (!status.isConnected) document.body.append(status);
      status.style.left = `${Math.max(8, Math.min(p.clientX + 12, innerWidth - 260))}px`;
      status.style.top = `${Math.max(8, Math.min(p.clientY + 20, innerHeight - 48))}px`;
      status.textContent = 'Move outside the calendar to cancel';
      if (!valid || !hit) return;
      const day = new Date(`${hit.dataset.dropDate}T00:00:00`);
      targetStart = atStart ? new Date(Math.min(day.getTime(), addDays(end, -1).getTime())) : start;
      targetEnd = atStart ? end : new Date(Math.max(addDays(day, 1).getTime(), addDays(start, 1).getTime()));
      status.textContent = `${format(targetStart, 'MMM d')} – ${format(addDays(targetEnd, -1), 'MMM d')} · All day`;
      for (const destination of cells) {
        const date = new Date(`${destination.dataset.dropDate}T00:00:00`);
        if (date < targetStart || date >= targetEnd) continue;
        const preview = document.createElement('div');
        preview.className = 'all-day-resize-preview';
        preview.setAttribute('aria-hidden', 'true');
        preview.textContent = original.event.name || 'Untitled event';
        Object.assign(preview.style, { top: `${offset}px`, height: `${height}px`, background, color: ink });
        destination.append(preview);
        previews.push(preview);
      }
    }
    function up(p: PointerEvent) {
      if (p.pointerId !== e.pointerId) return;
      const changed = dragging && valid && (+targetStart !== +start || +targetEnd !== +end);
      cleanup();
      if (changed) original.onresize(original.event, targetStart, targetEnd);
    }
    function key(e: KeyboardEvent) { if (e.key === 'Escape') cleanup(); }
    cleanup = () => {
      clearPreviews(); status.remove();
      suppressClick = true;
      clearTimeout(timer);
      timer = setTimeout(() => (suppressClick = false), 0);
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      window.removeEventListener('pointercancel', cleanup);
      window.removeEventListener('blur', cleanup);
      window.removeEventListener('keydown', key);
      cleanup = () => {};
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
    window.addEventListener('pointercancel', cleanup);
    window.addEventListener('blur', cleanup);
    window.addEventListener('keydown', key);
  }
  function click(e: MouseEvent) {
    if (suppressClick || (e.target as Element).closest('.event-resize-handle')) {
      e.preventDefault(); e.stopImmediatePropagation();
    }
  }
  handles.forEach(handle => handle.addEventListener('pointerdown', down));
  node.addEventListener('click', click, true);
  return {
    update(next: Options) { cleanup(); options = next; refresh(); },
    destroy() { cleanup(); clearTimeout(timer); handles.forEach(handle => handle.remove()); node.removeEventListener('click', click, true); },
  };
}
