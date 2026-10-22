import { describe, expect, it } from 'vitest'
import {
  importsModelsOnCreate,
  isManagedModelCatalogClientType,
  isManagedOAuthClientType,
  LLM_CLIENT_TYPE_LIST,
  MANUAL_LLM_CLIENT_TYPE_LIST,
  suggestedModelCompatibilities,
} from './client-types'

describe('LLM client type lists', () => {
  it('offers managed OAuth types for new providers but keeps them out of manual selectors', () => {
    const allTypes = LLM_CLIENT_TYPE_LIST.map(type => type.value)
    const manualTypes = MANUAL_LLM_CLIENT_TYPE_LIST.map(type => type.value)

    expect(allTypes).toEqual(expect.arrayContaining(['openai-codex', 'github-copilot']))
    expect(manualTypes).not.toContain('openai-codex')
    expect(manualTypes).not.toContain('github-copilot')
    expect(manualTypes).toContain('openai-completions')
  })

  it('identifies only the dedicated OAuth provider types', () => {
    expect(isManagedOAuthClientType('openai-codex')).toBe(true)
    expect(isManagedOAuthClientType('github-copilot')).toBe(true)
    expect(isManagedOAuthClientType('openai-completions')).toBe(false)
    expect(isManagedOAuthClientType(undefined)).toBe(false)
  })

  it('imports models on creation only for providers that need no sign-in first', () => {
    expect(importsModelsOnCreate('openai-codex')).toBe(false)
    expect(importsModelsOnCreate('github-copilot')).toBe(false)
    expect(importsModelsOnCreate('openai-completions')).toBe(true)
    expect(importsModelsOnCreate('opencode-go')).toBe(true)
  })

  it('uses managed OAuth providers for account-scoped model catalogs', () => {
    expect(isManagedModelCatalogClientType('openai-codex')).toBe(true)
    expect(isManagedModelCatalogClientType('github-copilot')).toBe(true)
    expect(isManagedModelCatalogClientType('anthropic-messages')).toBe(false)
  })

  it('suggests agent capabilities without implying vision for manual client types', () => {
    for (const clientType of [
      'openai-responses',
      'openai-completions',
      'anthropic-messages',
      'google-generative-ai',
    ]) {
      expect(suggestedModelCompatibilities(clientType)).toEqual(['tool-call', 'reasoning'])
    }

    expect(suggestedModelCompatibilities('openai-codex')).toEqual([])
    expect(suggestedModelCompatibilities('edge-speech')).toEqual([])
    expect(suggestedModelCompatibilities('unknown')).toEqual([])
  })
})
