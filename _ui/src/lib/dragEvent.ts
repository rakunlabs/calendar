import { addDays, differenceInCalendarDays, format } from 'date-fns';
import type { CalendarEvent } from './api';
import { dateInput } from './calendar';

type Options = {
  event: CalendarEvent;
  day: Date;
  enabled: boolean;
  onmove: (event: CalendarEvent, target: Date) => void;
};

// Pointer events keep click-to-edit intact and allow one shared gesture in both views.
export function dragEvent(node: HTMLButtonElement, options: Options) {
  let cleanup = () => {};
  let suppressClick = false;
  let clickTimer: ReturnType<typeof setTimeout>;
  function down(e: PointerEvent) {
    if ((e.target as Element).closest('.event-resize-handle')) return;
    if (!options.enabled || e.button !== 0 || !e.isPrimary || e.pointerType === 'touch') return;
    cleanup();
    const original = options;
    let dragging = false;
    let target: Date | null = null;
    let highlighted: HTMLElement | null = null;
    const preview = document.createElement('div');
    preview.className = 'event-drag-preview';
    preview.setAttribute('aria-hidden', 'true');
    preview.inert = true;
    const status = document.createElement('div');
    status.className = 'event-drag-status';
    status.setAttribute('role', 'status');
    const bounds = node.getBoundingClientRect();
    const grabX = e.clientX - bounds.left;
    const grabY = e.clientY - bounds.top;
    const root = node.closest('.calendar-surface');
    const column = node.closest<HTMLElement>('.week-column');
    const anchor = column ? Math.floor((e.clientY - column.getBoundingClientRect().top) / 28) : 0;
    function move(p: PointerEvent) {
      if (p.pointerId !== e.pointerId) return;
      if (!dragging && Math.hypot(p.clientX - e.clientX, p.clientY - e.clientY) < 5) return;
      if (!dragging) {
        // Snapshot before dimming the source, including styles inherited from its view.
        const card = node.cloneNode(true) as HTMLButtonElement;
        const sources = [node, ...node.querySelectorAll<HTMLElement>('*')];
        const copies = [card, ...card.querySelectorAll<HTMLElement>('*')];
        sources.forEach((source, index) => {
          const copy = copies[index];
          const style = getComputedStyle(source);
          for (const property of style) copy.style.setProperty(property, style.getPropertyValue(property));
          copy.removeAttribute('id');
        });
        Object.assign(card.style, {
          position: 'static', margin: '0', width: `${bounds.width}px`,
          height: `${bounds.height}px`, minWidth: '0', maxWidth: 'none',
          boxSizing: 'border-box', transform: 'none', transition: 'none',
          pointerEvents: 'none',
        });
        preview.append(card);
        document.body.append(preview, status);
        dragging = true;
        node.classList.add('event-dragging');
      }
      preview.style.left = `${p.clientX - grabX}px`;
      preview.style.top = `${p.clientY - grabY}px`;
      status.style.left = `${Math.max(8, Math.min(p.clientX + 14, window.innerWidth - status.offsetWidth - 8))}px`;
      status.style.top = `${Math.max(8, Math.min(p.clientY + bounds.height - grabY + 8, window.innerHeight - status.offsetHeight - 8))}px`;
      status.textContent = 'Move outside the calendar to cancel';
      highlighted?.classList.remove('event-drop-target');
      target = null;
      const hit = document.elementFromPoint(p.clientX, p.clientY)?.closest<HTMLElement>('[data-drop-date]');
      if (!hit || !root?.contains(hit)) return;
      const day = new Date(`${hit.dataset.dropDate}T00:00:00`);
      if (hit.classList.contains('week-column')) {
        if (original.event.all_day) return;
        const slot = Math.max(
          0,
          Math.min(47, Math.floor((p.clientY - hit.getBoundingClientRect().top) / 28)),
        );
        const from = new Date(original.event.date_from);
        const offset =
          anchor * 30 -
          (differenceInCalendarDays(from, original.day) * 1440 + from.getHours() * 60 + from.getMinutes());
        day.setMinutes(Math.round((slot * 30 - offset) / 30) * 30);
        target = day;
      } else {
        if (hit.classList.contains('all-day-drop') && !original.event.all_day) return;
        const from = original.event.all_day
          ? new Date(`${dateInput(original.event.date_from, original.event.tz || 'UTC', true)}T00:00:00`)
          : new Date(original.event.date_from);
        target = addDays(from, differenceInCalendarDays(day, original.day));
      }
      status.textContent = format(
        target,
        original.event.all_day ? "EEE, MMM d · 'All day'" : 'EEE, MMM d · HH:mm',
      );
      highlighted = hit;
      hit.classList.add('event-drop-target');
    }
    function up(p: PointerEvent) {
      if (p.pointerId !== e.pointerId) return;
      const destination = target;
      const didDrag = dragging;
      cleanup();
      if (didDrag) {
        suppressClick = true;
        clearTimeout(clickTimer);
        clickTimer = setTimeout(() => (suppressClick = false), 0);
        if (destination) original.onmove(original.event, destination);
      }
    }
    function key(e: KeyboardEvent) {
      if (e.key === 'Escape') cleanup();
    }
    cleanup = () => {
      node.classList.remove('event-dragging');
      preview.remove();
      status.remove();
      highlighted?.classList.remove('event-drop-target');
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      window.removeEventListener('pointercancel', cleanup);
      window.removeEventListener('blur', cleanup);
      window.removeEventListener('keydown', key);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
    window.addEventListener('pointercancel', cleanup);
    window.addEventListener('blur', cleanup);
    window.addEventListener('keydown', key);
  }
  function click(e: MouseEvent) {
    if (suppressClick) {
      e.preventDefault();
      e.stopImmediatePropagation();
      suppressClick = false;
    }
  }
  node.addEventListener('pointerdown', down);
  node.addEventListener('click', click, true);
  return {
    update(next: Options) {
      options = next;
      if (!next.enabled) cleanup();
    },
    destroy() {
      cleanup();
      clearTimeout(clickTimer);
      node.removeEventListener('pointerdown', down);
      node.removeEventListener('click', click, true);
    },
  };
}
