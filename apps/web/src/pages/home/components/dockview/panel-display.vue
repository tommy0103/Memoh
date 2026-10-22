<template>
  <DockPanelFrame>
    <DisplayPane
      v-if="currentBotId"
      :key="currentBotId"
      :bot-id="currentBotId"
      :tab-id="props.params.api.id"
      :title="props.params.api.title ?? ''"
      :active="visible"
      @close="props.params.api.close()"
      @snapshot="handleSnapshot"
    />
  </DockPanelFrame>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { storeToRefs } from 'pinia'
import type { DockviewApi, DockviewPanelApi } from 'dockview-vue'
import { useChatStore } from '@/store/chat-list'
import { useDisplaySnapshotsStore } from '@/store/display-snapshots'
import DisplayPane from '../display-pane.vue'
import { usePanelVisible } from './use-panel-visible'
import DockPanelFrame from './panel-frame.vue'

// Keep the panel mounted for dock layout state; DisplayPane acquires a viewer
// only while this panel and the browser document are visible.
const props = defineProps<{
  params: {
    params: Record<string, unknown>
    api: DockviewPanelApi
    containerApi: DockviewApi
  }
}>()

const chatStore = useChatStore()
const displaySnapshots = useDisplaySnapshotsStore()
const { currentBotId } = storeToRefs(chatStore)

const panelVisible = usePanelVisible(props.params.api)
const route = useRoute()
// Settings cover the persistent chat workspace without unmounting its panels.
const visible = computed(() => panelVisible.value && (route.name === 'home' || route.name === 'bot'))

function handleSnapshot(payload: { tabId: string; sessionId?: string; dataUrl: string }) {
  const botId = currentBotId.value
  if (!botId) return
  displaySnapshots.upsert(botId, payload)
}
</script>
