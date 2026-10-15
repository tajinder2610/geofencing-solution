import type { LiveAlert } from '../api/types'
import type { SocketStatus } from '../hooks/useAlertSocket'
import { categoryColor, categoryLabel, formatCoord, formatDateTime } from '../lib/format'
import { Badge, Empty } from './ui'

interface Props {
  alerts: LiveAlert[]
  status: SocketStatus
  onClear: () => void
}

export function AlertFeed({ alerts, status, onClear }: Props) {
  return (
    <section className="card feed">
      <div className="card-head">
        <h2>
          Live alerts <span className="muted">({alerts.length})</span>
        </h2>
        <button className="btn btn-small" onClick={onClear} disabled={alerts.length === 0}>
          Clear
        </button>
      </div>
      {alerts.length === 0 ? (
        <Empty title={status === 'open' ? 'Listening for alerts…' : 'Connecting to the alert stream…'}>
          Create an alert rule, then send a location update that crosses the geofence boundary — the alert appears here instantly.
        </Empty>
      ) : (
        <ul className="feed-list">
          {alerts.map((a) => (
            <li key={a.event_id} className={`alert-card alert-${a.event_type}`}>
              <div className="alert-icon">{a.event_type === 'entry' ? '↘' : '↖'}</div>
              <div className="alert-body">
                <div className="alert-title">
                  <strong>{a.vehicle.vehicle_number}</strong>{' '}
                  {a.event_type === 'entry' ? 'entered' : 'exited'} <strong>{a.geofence.geofence_name}</strong>
                </div>
                <div className="alert-meta">
                  <Badge color={categoryColor(a.geofence.category)}>{categoryLabel(a.geofence.category)}</Badge>
                  <span>Driver: {a.vehicle.driver_name}</span>
                  <span>
                    📍 {formatCoord(a.location.latitude)}, {formatCoord(a.location.longitude)}
                  </span>
                  <span>🕒 {formatDateTime(a.timestamp)}</span>
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
