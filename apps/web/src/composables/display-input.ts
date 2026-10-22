type Input = Record<string, unknown>

// Release held input when the display loses focus, reconnects, or handles a paste.
export function createDisplayInput(send: (input: Input) => void) {
  let suppressPointer = false
  let pointerMask = 0
  let pointer: Input | null = null
  const keys = new Set<number>()

  function forward(input: Input) {
    if (input.type === 'key') {
      if (input.down) keys.add(input.keysym as number)
      else keys.delete(input.keysym as number)
    }
    if (input.type === 'pointer') pointer = input
    send(input)
  }

  function release() {
    for (const keysym of keys) send({ type: 'key', keysym, down: false })
    keys.clear()
    if (pointer) send({ ...pointer, button_mask: 0 })
    pointer = null
  }

  function cancel() {
    suppressPointer = pointerMask !== 0
    release()
  }

  function input(value: Input) {
    if (value.type === 'pointer') pointerMask = value.button_mask as number
    if (value.type === 'pointer' && suppressPointer) {
      if (value.button_mask !== 0) return
      suppressPointer = false
    }
    forward(value)
  }

  return { input, cancel }
}
