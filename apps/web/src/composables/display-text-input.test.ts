import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createDisplayTextInput } from './display-text-input'

describe('local IME commit boundary', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  function setup() {
    const text = vi.fn()
    let active = true
    const input = createDisplayTextInput({ active: () => active, text, release: vi.fn() })
    return { input, text, leave: () => { active = false } }
  }

  it('lets blur cancel a compositionend delivered before blur', () => {
    const { input, text } = setup()
    input.start()
    input.end('shi jie')
    vi.advanceTimersByTime(15) // native pointerdown arrives in a later task
    input.cancel()
    input.input('shi jie', false)
    vi.runAllTimers()
    expect(text).not.toHaveBeenCalled()
  })

  it('does not revive canceled preedit when focus returns before a late input', () => {
    const { input, text } = setup()
    input.start()
    input.cancel()
    input.end('shijie')
    input.input('shijie', false)
    vi.runAllTimers()
    expect(text).not.toHaveBeenCalled()
    input.start()
    input.end('世界')
    vi.runAllTimers()
    expect(text.mock.calls).toEqual([['世界']])
  })

  it('accepts input without a keydown once the canceled preedit settles', () => {
    const { input, text } = setup()
    input.start()
    input.cancel()
    input.input('shijie', false) // late input of the abandoned preedit
    vi.runAllTimers()
    input.input('🙂', false) // emoji picker or dictation sends no keydown
    vi.runAllTimers()
    expect(text.mock.calls).toEqual([['🙂']])
  })

  it.each(['input-first', 'end-first'])('commits a confirmed candidate once: %s', (order) => {
    const { input, text } = setup()
    input.start()
    if (order === 'input-first') input.input('你好', false)
    input.end('你好')
    if (order === 'end-first') input.input('你好', false)
    vi.runAllTimers()
    expect(text.mock.calls).toEqual([['你好']])
  })

  it('preserves two confirmations, including identical consecutive candidates', () => {
    const { input, text } = setup()
    for (let i = 0; i < 2; i++) {
      input.start()
      input.end('你好')
      input.input('你好', false)
    }
    vi.runAllTimers()
    expect(text.mock.calls).toEqual([['你好'], ['你好']])
  })

  it('does not suppress identical ordinary input events', () => {
    const { input, text } = setup()
    input.cancel() // initial pointer focus, with no preedit to abandon
    input.input('🙂', false)
    input.input('🙂', false)
    vi.runAllTimers()
    expect(text.mock.calls).toEqual([['🙂'], ['🙂']])
  })

  it('flushes confirmed text before a subsequent editing key', () => {
    const { input, text } = setup()
    input.start()
    input.end('你好')
    expect(input.keydown({ key: 'ArrowLeft', code: 'ArrowLeft', keyCode: 37, isComposing: false })).toBe(false)
    expect(text.mock.calls).toEqual([['你好']])
    vi.runAllTimers()
    expect(text).toHaveBeenCalledTimes(1)
  })

  it('drops a queued commit when the pane is no longer active', () => {
    const { input, text, leave } = setup()
    input.start()
    input.end('你好')
    leave()
    vi.runAllTimers()
    expect(text).not.toHaveBeenCalled()
  })
})
