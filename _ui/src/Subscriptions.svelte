<script lang="ts">
  import { normalizeFeedURL, type Subscription } from './lib/subscriptions';
  let {
    subscriptions,
    onchange,
    onrefresh,
    loading,
    errors,
  }: {
    subscriptions: Subscription[];
    onchange: (items: Subscription[]) => boolean;
    onrefresh: () => void;
    loading: boolean;
    errors: string[];
  } = $props();
  let expanded = $state(false);
  let name = $state('');
  let url = $state('');
  let error = $state('');
  function add() {
    error = '';
    try {
      const normalized = normalizeFeedURL(url);
      if (subscriptions.some((s) => s.url === normalized))
        throw new Error('This calendar is already subscribed.');
      if (
        onchange([
          ...subscriptions,
          { id: crypto.randomUUID(), name: name.trim(), url: normalized, enabled: true },
        ])
      ) {
        name = '';
        url = '';
        expanded = false;
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Invalid calendar URL.';
    }
  }
</script>

<section class="calendar-groups subscriptions" aria-labelledby="subscriptions-heading">
  <div class="section-heading">
    <h2 id="subscriptions-heading">Subscribed calendars</h2>
    <span>{subscriptions.length}</span>
  </div>
  <p class="sidebar-hint">
    Read-only ICS feeds. Saved in this browser; refreshed every 5 minutes while open, independently of the
    entity filter.
  </p>
  {#each subscriptions as subscription}
    <div class="subscription-row">
      <label class="group-filter"
        ><input
          type="checkbox"
          checked={subscription.enabled}
          onchange={() =>
            onchange(
              subscriptions.map((s) => (s.id === subscription.id ? { ...s, enabled: !s.enabled } : s)),
            )}
        />
        <span>{subscription.name}</span></label
      >
      <button
        class="icon-button small"
        aria-label={`Unsubscribe from ${subscription.name}`}
        onclick={() => onchange(subscriptions.filter((s) => s.id !== subscription.id))}>×</button
      >
    </div>
  {/each}
  {#if loading}<p class="sidebar-hint" role="status">Refreshing subscriptions…</p>{/if}
  {#each errors as message}<p class="sidebar-hint" role="alert">{message}</p>{/each}
  <button class="secondary-button" aria-expanded={expanded} onclick={() => (expanded = !expanded)}
    >Add subscription</button
  >
  {#if subscriptions.length}<button class="secondary-button" disabled={loading} onclick={onrefresh}
      >Refresh feeds</button
    >{/if}
  {#if expanded}
    <form
      class="subscription-form"
      onsubmit={(e) => {
        e.preventDefault();
        add();
      }}
    >
      <label for="feed-name">Calendar name</label><input
        id="feed-name"
        bind:value={name}
        required
        maxlength="100"
        placeholder="e.g. Team calendar"
      />
      <label for="feed-url">ICS / webcal URL</label><input
        id="feed-url"
        bind:value={url}
        required
        maxlength="8000"
        placeholder="https://…/calendar.ics"
      />
      <p class="sidebar-hint">
        Use the provider's published ICS link. Events must be edited at their source.
      </p>
      {#if error}<p class="sidebar-hint" role="alert">{error}</p>{/if}
      <button class="primary-button" disabled={!name.trim() || !url.trim()}>Subscribe</button>
    </form>
  {/if}
</section>
