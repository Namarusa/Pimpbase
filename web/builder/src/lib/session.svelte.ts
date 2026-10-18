import type { User } from './api'

export const session = $state<{ user: User | null; ready: boolean }>({ user: null, ready: false })

export const isBuilder = () => ['owner', 'admin', 'builder'].includes(session.user?.role ?? '')
export const isAdmin = () => ['owner', 'admin'].includes(session.user?.role ?? '')
