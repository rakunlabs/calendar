export interface Subscription {
  id: string;
  name: string;
  url: string;
  enabled: boolean;
}
const key = `calendar-subscriptions:${import.meta.env.BASE_URL}`;

export function normalizeFeedURL(value: string) {
  const url = new URL(value.trim().replace(/^webcal:\/\//i, 'https://'));
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password)
    throw new Error('Use an HTTP, HTTPS or webcal ICS URL without embedded credentials.');
  url.hash = '';
  return url.href;
}

export function loadSubscriptions(): Subscription[] {
  const data: unknown = JSON.parse(localStorage.getItem(key) || '[]');
  if (!Array.isArray(data)) throw new Error('Saved subscriptions could not be read.');
  return data.filter(
    (s): s is Subscription =>
      s &&
      typeof s.id === 'string' &&
      typeof s.name === 'string' &&
      typeof s.url === 'string' &&
      typeof s.enabled === 'boolean',
  );
}

export function storeSubscriptions(items: Subscription[]) {
  localStorage.setItem(key, JSON.stringify(items));
}
