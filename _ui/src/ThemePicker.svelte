<script lang="ts">
  import { onMount } from 'svelte';
  import { Monitor, Sun, Moon } from '@lucide/svelte';

  type Theme = 'system' | 'light' | 'dark';
  const options = [
    { value: 'system', label: 'System', icon: Monitor },
    { value: 'light', label: 'Light', icon: Sun },
    { value: 'dark', label: 'Dark', icon: Moon },
  ] as const;
  const normalize = (value: string | null | undefined): Theme =>
    value === 'light' || value === 'dark' ? value : 'system';
  let preference = $state<Theme>(normalize(document.documentElement.dataset.themePreference));
  const index = $derived(options.findIndex((option) => option.value === preference));
  const current = $derived(options[index]);
  const next = $derived(options[(index + 1) % options.length]);

  function apply() {
    const dark =
      preference === 'dark' ||
      (preference === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
    document.documentElement.dataset.themePreference = preference;
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', dark ? '#242424' : '#EDF2F4');
  }

  onMount(() => {
    const media = matchMedia('(prefers-color-scheme: dark)');
    const storage = (event: StorageEvent) => {
      if (event.key !== 'calendar.theme' && event.key !== null) return;
      preference = normalize(event.newValue);
      apply();
    };
    apply();
    media.addEventListener('change', apply);
    window.addEventListener('storage', storage);
    return () => {
      media.removeEventListener('change', apply);
      window.removeEventListener('storage', storage);
    };
  });

  function cycle() {
    preference = next.value;
    apply();
    try {
      localStorage.setItem('calendar.theme', preference);
    } catch {
      /* Storage is optional. */
    }
  }
</script>

<button
  class="icon-button theme-trigger"
  aria-label={`Theme: ${current.label}. Switch to ${next.label}`}
  title={`Theme: ${current.label}. Switch to ${next.label}`}
  onclick={cycle}><current.icon size={18} /></button
>

<style>
  .theme-trigger {
    width: 36px;
    height: 36px;
  }
</style>
