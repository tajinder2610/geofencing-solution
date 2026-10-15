import { describe, expect, it } from 'vitest'
import { closeRing, parseCoordinateText } from './format'

describe('parseCoordinateText', () => {
  it('parses plain lines', () => {
    expect(parseCoordinateText('1.5, 2.5\n3,4\n5, 6').points).toEqual([[1.5, 2.5], [3, 4], [5, 6]])
  })
  it('accepts JSON-ish pairs and drops an already-closing point', () => {
    const r = parseCoordinateText('[1, 2],\n[3, 4],\n[5, 6],\n[1, 2]')
    expect(r.error).toBeUndefined()
    expect(r.points).toEqual([[1, 2], [3, 4], [5, 6]])
  })
  it('rejects out-of-range and malformed lines', () => {
    expect(parseCoordinateText('91, 0').error).toMatch(/latitude/)
    expect(parseCoordinateText('0, 181').error).toMatch(/longitude/)
    expect(parseCoordinateText('hello').error).toMatch(/Line 1/)
  })
})

describe('closeRing', () => {
  it('appends the first point', () => {
    expect(closeRing([[1, 2], [3, 4], [5, 6]])).toEqual([[1, 2], [3, 4], [5, 6], [1, 2]])
  })
})
