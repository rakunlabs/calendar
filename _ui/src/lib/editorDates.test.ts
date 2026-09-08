import { describe, expect, it } from 'vitest';
import { moveEditorEnd, shiftEditorDate } from './editorDates';
import { toInstant } from './calendar';

describe('editor all-day dates', () => {
  it('converts inclusive dates to exclusive API dates and back', () => {
    expect(shiftEditorDate('2026-12-31', 1)).toBe('2027-01-01');
    expect(shiftEditorDate('2027-01-01', -1)).toBe('2026-12-31');
    expect(shiftEditorDate('2028-02-28', 1)).toBe('2028-02-29');
  });

  it('uses zone midnights, not 24-hour offsets, over DST', () => {
    const start = toInstant('2026-03-08', 'America/New_York');
    const end = toInstant(shiftEditorDate('2026-03-08', 1), 'America/New_York');
    expect(Date.parse(end) - Date.parse(start)).toBe(23 * 3600000);
  });

  it('preserves inclusive calendar-day duration when moving the start', () => {
    expect(moveEditorEnd('2026-03-07', '2026-03-09', '2026-10-31', 'America/New_York', true)).toBe(
      '2026-11-02',
    );
    expect(moveEditorEnd('2026-03-07', '2026-03-07', '2026-03-08', 'UTC', true)).toBe('2026-03-08');
  });
});

describe('editor timed duration', () => {
  it('preserves fractional-hour duration across midnight', () => {
    expect(moveEditorEnd('2026-09-07T09:30', '2026-09-07T11:00', '2026-09-07T23:30', 'UTC', false)).toBe(
      '2026-09-08T01:00:00',
    );
  });

  it('preserves elapsed duration across a DST transition', () => {
    expect(
      moveEditorEnd('2026-03-07T01:30', '2026-03-07T03:30', '2026-03-08T01:30', 'America/New_York', false),
    ).toBe('2026-03-08T04:30:00');
  });

  it('rejects nonexistent start times instead of silently shifting them', () => {
    expect(() =>
      moveEditorEnd('2026-03-07T01:30', '2026-03-07T03:30', '2026-03-08T02:30', 'America/New_York', false),
    ).toThrow('does not exist');
  });

  it('leaves an invalid end for the user to correct', () => {
    expect(moveEditorEnd('2026-09-07T10:00', '2026-09-07T09:00', '2026-09-08T10:00', 'UTC', false)).toBe(
      '2026-09-07T09:00',
    );
  });
});
