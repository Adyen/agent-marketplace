import type { Configuration, SessionEvent, Snapshot } from './api/client'
import type { Row } from './controller'

export function snapshot(rows: Row[] = [], overrides: Partial<Snapshot> = {}): Snapshot {
  return {
    stage: 'final',
    drift: 'ready',
    rows,
    groups: [
      { id: 'required', title: 'Required' },
      { id: 'enabled', title: 'Enabled' },
      { id: 'pending', title: 'Pending sync' },
      { id: 'stale', title: 'Stale' },
      { id: 'broken', title: 'Broken' },
      { id: 'available', title: 'Available' },
      { id: 'malformed', title: 'Malformed' },
    ],
    diagnostics: [],
    harnesses: ['devin'],
    pendingChanges: 0,
    discoveryEnabled: false,
    discoveryHealth: [],
    discoveryQuery: '',
    discoveryRequestId: 0,
    discoverySequence: 0,
    discoveryLoading: false,
    operation: null,
    ...overrides,
  }
}

export function row(name: string, overrides: Partial<Row> = {}): Row {
  return {
    id: `skill/${name}#${name.length.toString(16)}`,
    name,
    displayName: name,
    searchName: name.toLowerCase(),
    searchWords: [name.toLowerCase()],
    kind: 'skill',
    description: `${name} description`,
    registry: 'local',
    source: { id: 'local', label: '(local)' },
    scope: 'user',
    required: false,
    pendingVerification: false,
    checked: true,
    harnessOverride: false,
    harnesses: ['devin'],
    status: 'enabled',
    group: 'enabled',
    discovery: false,
    inspectable: false,
    installedFiles: false,
    resourceAction: 'none',
    ...overrides,
  }
}

export function event(overrides: Partial<SessionEvent>): SessionEvent {
  return { type: 'snapshot', sequence: 0, generation: 1, operation: null, payload: snapshot(), ...overrides } as SessionEvent
}

export function configuration(overrides: Partial<Configuration> = {}): Configuration {
  return {
    userTargets: ['devin'],
    organizationTargets: [],
    userSources: [],
    organizationSources: [],
    supportedTargets: ['devin'],
    rootPresent: true,
    editable: true,
    managedOutsideGUI: [],
    firstRun: false,
    canInitialize: false,
    detectedTargets: [],
    detectionUnavailable: false,
    consentRequired: false,
    telemetryDestination: 'not configured',
    retentionPolicyURL: 'not configured',
    ...overrides,
  }
}
