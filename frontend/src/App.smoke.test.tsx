// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { AppDataProvider } from './hooks/useAppData'
import { ToastProvider } from './hooks/useToasts'

// Leaflet's vector layers need real SVG, which jsdom lacks, so the map is replaced by a stub that
// exposes the same callbacks (everything else in the app is real).
vi.mock('./components/MapView', () => ({
  MapView: (p: {
    mode: string
    geofences: unknown[]
    vehicles: unknown[]
    draft: unknown[]
    picked: unknown
    onDraftAdd: (pt: [number, number]) => void
    onPick: (pt: [number, number]) => void
  }) => (
    <div className="leaflet-container" data-testid="map" data-mode={p.mode} data-geofences={p.geofences.length} data-vehicles={p.vehicles.length} data-draft={p.draft.length}>
      <button onClick={() => (p.mode === 'draw' ? p.onDraftAdd([12.5 + p.draft.length, 77.5]) : p.onPick([12.97, 77.59]))}>map-click</button>
    </div>
  ),
}))

// ---- fake WebSocket so the test can push alerts
class FakeSocket {
  static instances: FakeSocket[] = []
  onopen: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  constructor(public url: string) {
    FakeSocket.instances.push(this)
    queueMicrotask(() => this.onopen?.())
  }
  close() {
    this.onclose?.()
  }
  push(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) })
  }
}

const square = [[37.7749, -122.4194], [37.7849, -122.4194], [37.7849, -122.4094], [37.7749, -122.4094], [37.7749, -122.4194]]
const geofence = { id: 'geo_1', name: 'Downtown Restricted', description: 'No trucks', coordinates: square, category: 'restricted_zone', created_at: '2025-01-15T10:00:00Z' }
const vehicle = {
  id: 'veh_1', vehicle_number: 'KA-01-AB-1234', driver_name: 'John Doe', vehicle_type: 'truck', phone: '+1234567890', status: 'active',
  created_at: '2025-01-15T09:00:00Z', current_location: { latitude: 37.7799, longitude: -122.4144, timestamp: '2025-01-15T10:35:00Z' },
  current_geofences: [{ geofence_id: 'geo_1', geofence_name: 'Downtown Restricted', category: 'restricted_zone' }],
}

const calls: { method: string; path: string; body?: unknown }[] = []

function mockFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const path = url.replace('http://localhost:8080', '')
    const method = init?.method ?? 'GET'
    calls.push({ method, path, body: init?.body ? JSON.parse(init.body as string) : undefined })
    const ok = (data: unknown, status = 200) => ({ ok: status < 400, status, json: async () => data })
    if (path.startsWith('/geofences') && method === 'GET') return ok({ geofences: [geofence], total_count: 1 })
    if (path.startsWith('/vehicles/trail')) return ok({ vehicle_id: 'veh_1', points: [] })
    if (path.startsWith('/vehicles') && method === 'GET') return ok({ vehicles: [vehicle], total_count: 1 })
    if (method === 'DELETE' && path === '/vehicles/veh_1/trail') return ok({ cleared: true })
    if (method === 'DELETE' && path === '/vehicles/veh_1') return ok({ deleted: true })
    if (path.startsWith('/alerts') && method === 'GET') return ok({ alerts: [], total_count: 0 })
    if (path === '/alerts/configure') return ok({ alert_id: 'alert_1' }, 201)
    if (path.startsWith('/violations/history')) return ok({ violations: [], total_count: 0 })
    if (path === '/vehicles/location') return ok({ vehicle_id: 'veh_1', location_updated: true, current_geofences: [{ geofence_id: 'geo_1', geofence_name: 'Downtown Restricted', status: 'inside' }] })
    return ok({ error: 'unexpected ' + method + ' ' + path }, 500)
  }))
}

const renderApp = () =>
  render(
    <ToastProvider>
      <AppDataProvider>
        <App />
      </AppDataProvider>
    </ToastProvider>,
  )

beforeEach(() => {
  calls.length = 0
  FakeSocket.instances = []
  vi.stubGlobal('WebSocket', FakeSocket)
  mockFetch()
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('App smoke test', () => {
  it('loads data, shows the live connection, lists geofences and vehicles', async () => {
    renderApp()
    await waitFor(() => expect(screen.getByText('Live')).toBeTruthy())
    await waitFor(() => expect(screen.getByText('Downtown Restricted')).toBeTruthy())
    expect(screen.getByTestId('map').getAttribute('data-geofences')).toBe('1')
    expect(screen.getByTestId('map').getAttribute('data-vehicles')).toBe('1')

    await userEvent.click(screen.getByRole('tab', { name: /Vehicles/ }))
    await waitFor(() => expect(screen.getByText('KA-01-AB-1234')).toBeTruthy())
    expect(screen.getByText(/Inside: Downtown Restricted/)).toBeTruthy()
  })

  it('shows a toast, feed entry and unread badge when a live alert arrives', async () => {
    renderApp()
    await waitFor(() => expect(screen.getByText('Live')).toBeTruthy())

    act(() =>
      FakeSocket.instances[0].push({
        event_id: 'evt_1', event_type: 'entry', timestamp: '2025-01-15T10:35:00Z',
        vehicle: { vehicle_id: 'veh_1', vehicle_number: 'KA-01-AB-1234', driver_name: 'John Doe' },
        geofence: { geofence_id: 'geo_1', geofence_name: 'Downtown Restricted', category: 'restricted_zone' },
        location: { latitude: 37.7799, longitude: -122.4144 },
      }),
    )
    await waitFor(() => expect(screen.getByText(/KA-01-AB-1234 entered Downtown Restricted/)).toBeTruthy())
    expect(document.querySelector('.tab-count')?.textContent).toBe('1')

    await userEvent.click(screen.getByRole('tab', { name: /Live alerts/ }))
    const feed = document.querySelector('.feed') as HTMLElement
    expect(within(feed).getByText(/entered/)).toBeTruthy()
    expect(within(feed).getByText(/Driver: John Doe/)).toBeTruthy()
    expect(document.querySelector('.tab-count')).toBeNull() // unread cleared
  })

  it('creates an alert rule through the form', async () => {
    renderApp()
    await waitFor(() => expect(screen.getByText('Live')).toBeTruthy())
    await userEvent.click(screen.getByRole('tab', { name: /Alert rules/ }))
    const [geofenceSelect] = screen.getAllByRole('combobox')
    await userEvent.selectOptions(geofenceSelect, 'geo_1')
    await userEvent.click(screen.getByRole('button', { name: 'Create alert rule' }))
    await waitFor(() => expect(calls.some((c) => c.path === '/alerts/configure')).toBe(true))
    expect(calls.find((c) => c.path === '/alerts/configure')?.body).toEqual({ geofence_id: 'geo_1', event_type: 'entry' })
  })

  it('sends a location update and shows which geofences the vehicle is inside', async () => {
    renderApp()
    await waitFor(() => expect(screen.getByText('Live')).toBeTruthy())
    await userEvent.click(screen.getByRole('tab', { name: /Vehicles/ }))
    await waitFor(() => expect(screen.getByText('KA-01-AB-1234')).toBeTruthy())

    const timestampField = document.querySelector('input[type="datetime-local"]') as HTMLInputElement
    const initialTimestamp = timestampField.value
    const select = screen.getAllByRole('combobox')[0]
    await userEvent.selectOptions(select, 'veh_1')
    await userEvent.type(screen.getByPlaceholderText('37.7799'), '37.7799')
    await userEvent.type(screen.getByPlaceholderText('-122.4144'), '-122.4144')
    await new Promise((resolve) => setTimeout(resolve, 1100))
    await userEvent.click(screen.getByRole('button', { name: 'Send location update' }))

    await waitFor(() => expect(screen.getByText(/Inside/, { selector: 'li' })).toBeTruthy())
    const sent = calls.find((c) => c.path === '/vehicles/location')?.body as Record<string, unknown>
    expect(sent).toMatchObject({ vehicle_id: 'veh_1', latitude: 37.7799, longitude: -122.4144 })
    expect(String(sent.timestamp)).toMatch(/Z$/)
    expect(Date.parse(String(sent.timestamp))).toBeGreaterThan(Date.parse(initialTimestamp))
    expect(timestampField.value).not.toBe(initialTimestamp)
  })

  it('clears a vehicle trail or deletes the vehicle after confirmation', async () => {
    vi.stubGlobal('confirm', vi.fn(() => true))
    renderApp()
    await waitFor(() => expect(screen.getByText('Live')).toBeTruthy())
    await userEvent.click(screen.getByRole('tab', { name: /Vehicles/ }))
    await waitFor(() => expect(screen.getByText('KA-01-AB-1234')).toBeTruthy())

    await userEvent.click(screen.getByRole('button', { name: 'Clear trail' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.path === '/vehicles/veh_1/trail')).toBe(true))
    expect(screen.getByText('Trail cleared')).toBeTruthy()

    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.path === '/vehicles/veh_1')).toBe(true))
    expect(screen.getByText('Vehicle deleted')).toBeTruthy()
  })

  it('resets history filters and reloads all events from the first page', async () => {
    renderApp()
    await waitFor(() => expect(screen.getByText('Live')).toBeTruthy())
    await userEvent.click(screen.getByRole('tab', { name: /History/ }))

    const [vehicleFilter, geofenceFilter] = screen.getAllByRole('combobox')
    await userEvent.selectOptions(vehicleFilter, 'veh_1')
    await userEvent.selectOptions(geofenceFilter, 'geo_1')
    await userEvent.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() =>
      expect(calls.some((c) => c.path.includes('/violations/history?vehicle_id=veh_1&geofence_id=geo_1'))).toBe(true),
    )

    await userEvent.click(screen.getByRole('button', { name: 'Reset' }))

    await waitFor(() =>
      expect(calls.some((c) => c.path === '/violations/history?limit=25&offset=0')).toBe(true),
    )
    expect((vehicleFilter as HTMLSelectElement).value).toBe('')
    expect((geofenceFilter as HTMLSelectElement).value).toBe('')
  })

  it('requires 3 points before a geofence can be created, and accepts typed coordinates', async () => {
    renderApp()
    await waitFor(() => expect(screen.getByText('Downtown Restricted')).toBeTruthy())
    const submit = screen.getByRole('button', { name: 'Create geofence' }) as HTMLButtonElement
    expect(submit.disabled).toBe(true)

    await userEvent.type(screen.getByPlaceholderText('Downtown Delivery Zone'), 'Typed zone')
    const textarea = screen.getByRole('textbox', { name: /Coordinates/ })
    await userEvent.click(textarea)
    await userEvent.paste('1, 1\n1, 2\n2, 2')
    await waitFor(() => expect(submit.disabled).toBe(false))
    expect(screen.getByText(/3 points/)).toBeTruthy()
  })

  it('draws a geofence by clicking the map and submits a closed ring', async () => {
    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      const path = url.replace('http://localhost:8080', '')
      const method = init?.method ?? 'GET'
      calls.push({ method, path, body: init?.body ? JSON.parse(init.body as string) : undefined })
      const ok = (data: unknown, status = 200) => ({ ok: status < 400, status, json: async () => data })
      if (path === '/geofences' && method === 'POST') return ok({ id: 'geo_2', name: 'Drawn', status: 'active' }, 201)
      if (path.startsWith('/geofences')) return ok({ geofences: [geofence], total_count: 1 })
      if (path.startsWith('/vehicles')) return ok({ vehicles: [], total_count: 0 })
      return ok({ alerts: [], total_count: 0 })
    })
    renderApp()
    await waitFor(() => expect(screen.getByText('Downtown Restricted')).toBeTruthy())

    await userEvent.type(screen.getByPlaceholderText('Downtown Delivery Zone'), 'Drawn')
    await userEvent.click(screen.getByRole('button', { name: /Draw on map/ }))
    expect(screen.getByTestId('map').getAttribute('data-mode')).toBe('draw')
    for (let i = 0; i < 3; i++) await userEvent.click(screen.getByText('map-click'))
    await waitFor(() => expect(screen.getByTestId('map').getAttribute('data-draft')).toBe('3'))

    await userEvent.click(screen.getByRole('button', { name: 'Create geofence' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.path === '/geofences')).toBe(true))
    const body = calls.find((c) => c.method === 'POST' && c.path === '/geofences')!.body as { coordinates: number[][]; category: string }
    expect(body.coordinates).toHaveLength(4)
    expect(body.coordinates[0]).toEqual(body.coordinates[3]) // closed ring
    expect(body.category).toBe('restricted_zone')
  })

  it('clicking the map on the Vehicles tab fills the latitude/longitude inputs', async () => {
    renderApp()
    await waitFor(() => expect(screen.getByText('Live')).toBeTruthy())
    await userEvent.click(screen.getByRole('tab', { name: /Vehicles/ }))
    expect(screen.getByTestId('map').getAttribute('data-mode')).toBe('pick')
    await userEvent.click(screen.getByText('map-click'))
    await waitFor(() => expect((screen.getByPlaceholderText('37.7799') as HTMLInputElement).value).toBe('12.97'))
    expect((screen.getByPlaceholderText('-122.4144') as HTMLInputElement).value).toBe('77.59')
  })

  it('reconnects the WebSocket after the connection drops', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      renderApp()
      await waitFor(() => expect(screen.getByText('Live')).toBeTruthy())
      act(() => FakeSocket.instances[0].onclose?.())
      await waitFor(() => expect(screen.getByText('Reconnecting…')).toBeTruthy())
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1500)
      })
      await waitFor(() => expect(FakeSocket.instances.length).toBeGreaterThan(1))
      await waitFor(() => expect(screen.getByText('Live')).toBeTruthy())
    } finally {
      vi.useRealTimers()
    }
  })
})
