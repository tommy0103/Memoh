<template>
  <component
    :is="iconComponent"
    v-if="iconComponent"
    :size="size"
    v-bind="$attrs"
  />
  <img
    v-else-if="imageSource && imageSource !== failedSource"
    :src="imageSource"
    decoding="sync"
    loading="eager"
    :width="size"
    :height="size"
    alt=""
    class="[color-scheme:light] dark:[color-scheme:dark]"
    v-bind="$attrs"
    @error="onImageError"
  >
  <!-- URL icon still fetching: hold an empty, correctly-sized box instead of
       the fallback slot. The fallback would paint at the glyph's default size
       (it receives no $attrs) and then swap to the real image — a visible
       flash + size jump on every uncached mount. Unknown non-URL names still
       get the slot. -->
  <span
    v-else-if="isUrl"
    class="inline-block [&>svg]:size-full"
    v-bind="$attrs"
    aria-hidden="true"
  >
    <slot v-if="imageSource === null || (imageSource && imageSource === failedSource)" />
  </span>
  <slot v-else />
</template>

<script setup lang="ts">
import { computed, ref, type Component } from 'vue'
import { iconMap } from './icons.ts'
import { providerIconSource } from './preload'

const props = withDefaults(defineProps<{
  icon: string
  size?: string | number
}>(), {
  size: '1em',
})

defineOptions({ inheritAttrs: false })

const isUrl = computed(() =>
  props.icon.startsWith('http://') || props.icon.startsWith('https://'),
)

const source = computed(() => isUrl.value && typeof Image !== 'undefined'
  ? providerIconSource(props.icon)
  : undefined)
const imageSource = computed(() => source.value ? source.value.value : '')
const failedSource = ref('')

function onImageError(event: Event): void {
  // Preserve the caller's icon slot and sizing when the original URL also
  // fails. Record the failed node's source so a late event cannot hide a new URL.
  failedSource.value = (event.currentTarget as HTMLImageElement).getAttribute('src') ?? ''
}

const iconComponent = computed<Component | undefined>(() => {
  if (isUrl.value) return undefined
  return iconMap[props.icon]
})
</script>
