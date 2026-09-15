import { addDays, format } from 'date-fns';
import type { CalendarEvent } from './api';

type Options = {
  event: CalendarEvent;
  day: Date;
  enabled: boolean;
  onresize: (event: CalendarEvent, start: Date, end: Date) => void;
};

export function resizeEvent(node: HTMLButtonElement, options: Options) {
  const handle = document.createElement('span');
  handle.className = 'event-resize-handle';
  handle.title = 'Drag to change end time · Alt + ↑ / ↓ adjusts by 30 minutes';
  handle.setAttribute('aria-hidden', 'true');
  node.append(handle);
  const topHandle = document.createElement('span');
  topHandle.className = 'event-resize-handle event-resize-start';
  topHandle.title = 'Drag to change start time · Alt + Shift + ↑ / ↓ adjusts by 30 minutes';
  topHandle.setAttribute('aria-hidden', 'true');
  node.append(topHandle);
  let cleanup = () => {};
  let suppressClick = false;
  let timer: ReturnType<typeof setTimeout>;
  function available(atStart = false) {
    const start = Date.parse(options.event.date_from);
    const end = Date.parse(options.event.date_to);
    return options.enabled && !options.event.all_day && (atStart
      ? start >= options.day.getTime() && start < addDays(options.day, 1).getTime()
      : end > options.day.getTime() && end <= addDays(options.day, 1).getTime());
  }
  function refresh() { handle.hidden = !available(); topHandle.hidden = !available(true); }
  refresh();
  function down(e: PointerEvent) {
    const atStart = e.currentTarget === topHandle;
    if (!available(atStart) || e.button !== 0 || !e.isPrimary || e.pointerType === 'touch') return;
    e.preventDefault();
    e.stopPropagation();
    cleanup();
    const original = options;
    const column = node.closest<HTMLElement>('.week-column')!;
    const height = node.style.height;
    const top = node.style.top;
    const label = node.querySelector('span:not(.event-resize-handle)');
    const text = label?.textContent ?? '';
    const start = Date.parse(original.event.date_from);
    const end = Date.parse(original.event.date_to);
    let target = atStart ? start : end;
    let changed = false;
    function move(p: PointerEvent) {
      if (p.pointerId !== e.pointerId) return;
      if (!changed && Math.abs(p.clientY - e.clientY) < 5) return;
      changed = true;
      const minutes = Math.round((p.clientY - column.getBoundingClientRect().top) / 28) * 30;
      const date = new Date(original.day);
      date.setMinutes(Math.max(0, Math.min(1440, minutes)));
      target = atStart ? Math.min(end - 30 * 60_000, date.getTime()) : Math.max(start + 30 * 60_000, date.getTime());
      const nextStart = atStart ? target : start;
      const nextEnd = atStart ? end : target;
      const visibleStart = Math.max(nextStart, original.day.getTime());
      const visibleEnd = Math.min(nextEnd, addDays(original.day, 1).getTime());
      node.style.top = `${(visibleStart - original.day.getTime()) / 60_000 / 30 * 28}px`;
      node.style.height = `${(visibleEnd - visibleStart) / 60_000 / 30 * 28}px`;
      node.classList.add('event-resizing');
      if (label) label.textContent = `${format(new Date(nextStart), 'HH:mm')} - ${format(new Date(nextEnd), 'HH:mm')}`;
    }
    function up(p: PointerEvent) {
      if (p.pointerId !== e.pointerId) return;
      const destination = target;
      cleanup();
      if (changed && destination !== (atStart ? start : end))
        original.onresize(original.event, new Date(atStart ? destination : start), new Date(atStart ? end : destination));
    }
    function key(e: KeyboardEvent) { if (e.key === 'Escape') cleanup(); }
    cleanup = () => {
      node.style.height = height;
      node.style.top = top;
      node.classList.remove('event-resizing');
      if (label) label.textContent = text;
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
      e.preventDefault();
      e.stopImmediatePropagation();
    }
  }
  function key(e: KeyboardEvent) {
    if (!available(e.shiftKey) || !e.altKey || !['ArrowUp', 'ArrowDown'].includes(e.key)) return;
    e.preventDefault();
    const delta = (e.key === 'ArrowUp' ? -1 : 1) * 30 * 60_000;
    const start = Date.parse(options.event.date_from) + (e.shiftKey ? delta : 0);
    const end = Date.parse(options.event.date_to) + (e.shiftKey ? 0 : delta);
    if (end >= start + 30 * 60_000)
      options.onresize(options.event, new Date(start), new Date(end));
  }
  handle.addEventListener('pointerdown', down);
  topHandle.addEventListener('pointerdown', down);
  node.addEventListener('click', click, true);
  node.addEventListener('keydown', key);
  return {
    update(next: Options) { cleanup(); options = next; refresh(); },
    destroy() {
      cleanup(); clearTimeout(timer); handle.remove(); topHandle.remove();
      node.removeEventListener('click', click, true);
      node.removeEventListener('keydown', key);
    },
  };
}
