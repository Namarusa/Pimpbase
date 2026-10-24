// The integrations catalog and an app's connections, loaded once per
// page and shared by the Integrations tab and the flow editor.
import { api, type Connection, type IntegrationAction, type Manifest } from '../api'

export const integrations = $state({ catalog: [] as Manifest[], connections: [] as Connection[], app: '' })

let catalogLoad: Promise<Manifest[]> | null = null

export async function loadIntegrations(appId: string) {
  catalogLoad ??= api.integrationCatalog()
  const [catalog, connections] = await Promise.all([catalogLoad, api.connections(appId)])
  integrations.catalog = catalog
  integrations.connections = connections
  integrations.app = appId
}

export const manifestOf = (kind?: string) => integrations.catalog.find((m) => m.id === kind)
export const connectionOf = (id?: string) => integrations.connections.find((c) => c.id === id)

// actionOf finds what an integration step does.
export function actionOf(inputs: Record<string, any> | undefined): { conn?: Connection; manifest?: Manifest; action?: IntegrationAction } {
  const conn = connectionOf(inputs?.connection)
  const manifest = manifestOf(conn?.kind)
  return { conn, manifest, action: manifest?.actions.find((a) => a.key === inputs?.action) }
}

// mode says whether a connection's runs use test or live keys, from the
// shape of Stripe-style keys; other services say nothing.
export function keyMode(c: Connection, which: 'live' | 'test'): 'test' | 'live' | '' {
  const h = c.hints?.secretKey?.[which] ?? ''
  if (/_test_/.test(h)) return 'test'
  if (/_live_/.test(h)) return 'live'
  return ''
}
