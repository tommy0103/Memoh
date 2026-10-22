import type { AppItem } from '@/composables/api/useApps'
import { useI18n } from 'vue-i18n'
import type { DependencyItem } from '@/composables/api/useWorkspaceDependencies'
import { dependencyText } from '@/utils/workspace-dependency'
import { sdkApiUrl } from '@/lib/api-client'

type CatalogText = Pick<DependencyItem, 'id' | 'name' | 'description' | 'translations' | 'icon_url'>

export function useWorkspaceDependencyText() {
  const { locale } = useI18n()
  const dependencyIconUrl = (item: CatalogText) => {
    const path = item.icon_url ?? ''
    return /^\/workspace-dependencies\/icons\/[a-f0-9]{64}$/.test(path) ? sdkApiUrl({ url: path }) : ''
  }

  /** Discovered Apps have no release artwork; reuse their matching dependency's icon. */
  const appDependencyIconUrl = (item: AppItem) => {
    if (item.icon) return ''
    const dependencies = item.dependencies ?? []
    const dependency = dependencies.find(entry => entry.id === item.app_id) ?? dependencies[0]
    return dependency?.dependency ? dependencyIconUrl(dependency.dependency) : ''
  }

  return {
    dependencyName: (item: CatalogText) => dependencyText(item, 'name', locale.value),
    dependencyDescription: (item: CatalogText) => dependencyText(item, 'description', locale.value),
    dependencyIconUrl,
    appDependencyIconUrl,
  }
}
