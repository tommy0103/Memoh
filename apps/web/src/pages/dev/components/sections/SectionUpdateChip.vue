<script setup lang="ts">
// Update chip: footer update affordance driven by the DEV mock bridge, so every
// state (incl. the odd "restart happened, version unchanged" one) is testable
// without a real update feed. Arm the mock, flip states, then hover the chip.
// In the desktop dev shell the armed mock overrides the real bridge; disarming
// hands control back. Also reachable from any console: window.__memohMockUpdate.
import { Button } from '@felinic/ui'
import SectionShell from '../components/SectionShell.vue'
import Specimen from '../components/Specimen.vue'
import UpdateChip from '@/components/sidebar/update-chip.vue'
import { mockDesktopUpdates } from '@/lib/desktop-updates-mock'
import type { DesktopUpdateStatus } from '@/lib/desktop-shell'

const mock = mockDesktopUpdates

const states: { id: DesktopUpdateStatus; label: string }[] = [
  { id: 'idle', label: 'idle (hidden)' },
  { id: 'checking', label: 'checking (hidden)' },
  { id: 'up-to-date', label: 'up-to-date (hidden)' },
  { id: 'unavailable', label: 'unavailable (hidden)' },
  { id: 'installing', label: 'installing (disabled)' },
  { id: 'downloading', label: 'downloading 42%' },
  { id: 'downloaded', label: 'downloaded' },
  { id: 'error', label: 'error' },
]

function pick(status: DesktopUpdateStatus) {
  mock.setStatus(status)
}
</script>

<template>
  <SectionShell
    id="update-chip"
    label="Update chip"
    description="Footer update affordance: circle that grows into a text pill on hover. Driven by the dev mock bridge."
  >
    <Specimen label="mock console">
      <div class="flex flex-wrap items-center gap-2">
        <Button
          v-if="!mock.enabled.value"
          size="sm"
          @click="mock.arm()"
        >
          Arm mock
        </Button>
        <Button
          v-else
          size="sm"
          variant="outline"
          @click="mock.disarm()"
        >
          Disarm (back to real bridge)
        </Button>
        <template v-if="mock.enabled.value">
          <Button
            v-for="s in states"
            :key="s.id"
            size="sm"
            variant="outline"
            @click="pick(s.id)"
          >
            {{ s.label }}
          </Button>
          <Button
            size="sm"
            variant="outline"
            @click="mock.bridge.install()"
          >
            Begin mock restart
          </Button>
          <Button
            size="sm"
            variant="outline"
            @click="mock.setState({ status: 'downloading', progress: null })"
          >
            Unknown progress
          </Button>
          <Button
            size="sm"
            variant="outline"
            @click="mock.setState({ status: 'up-to-date', currentVersion: '9.9.9-mock', error: null })"
          >
            Restart succeeded
          </Button>
          <Button
            size="sm"
            variant="outline"
            @click="mock.setState({ status: 'error', error: 'The previous update was not applied. Retry from About.' })"
          >
            Restart unchanged
          </Button>
          <Button
            size="sm"
            variant="outline"
            @click="mock.bridge.setAutoUpdate(!mock.state.value.autoUpdate)"
          >
            Auto update: {{ mock.state.value.autoUpdate ? 'on' : 'off' }}
          </Button>
        </template>
      </div>
    </Specimen>

    <Specimen label="live chip (hover to expand)">
      <div class="flex items-center gap-2">
        <span class="flex h-9 min-w-0 flex-1 items-center rounded-md px-3 text-control text-muted-foreground">
          user block placeholder
        </span>
        <UpdateChip />
      </div>
      <p
        v-if="!mock.enabled.value"
        class="mt-2 text-body text-muted-foreground"
      >
        Arm the mock above to drive states. In the desktop dev shell the real
        bridge also renders here when a genuine update state exists.
      </p>
    </Specimen>
  </SectionShell>
</template>
