<script lang="ts">
  import { untrack, type ComponentProps } from 'svelte';
  import EventForm from './EventForm.svelte';
  import { hasExceptions } from './lib/recurrence';

  let props: ComponentProps<typeof EventForm> = $props();
  const canChooseScope = $derived(
    !!props.occurrence?.recurrence_id && !!props.event && (!!props.event.rrule || hasExceptions(props.event)),
  );
  let scope = $state<'occurrence' | 'series'>(untrack(() => (canChooseScope ? 'occurrence' : 'series')));
</script>

{#key scope}
  <EventForm
    {...props}
    master={props.event}
    event={scope === 'occurrence'
      ? { ...props.occurrence!, event_group: props.event!.event_group }
      : props.event}
    {scope}
    onscope={canChooseScope ? (next) => (scope = next) : undefined}
  />
{/key}
