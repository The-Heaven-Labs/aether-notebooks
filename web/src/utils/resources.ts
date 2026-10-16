import {
  BookOpen, Bot, Cpu, Database, Folder, LayoutDashboard, Plug, Sparkles, Wrench,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import type { FavoriteResourceType } from '../types'

/**
 * Shared catalog-resource vocabulary: the icon, label, and route for each of
 * the nine searchable/favoritable types. The search palette and the `/search`
 * page read the same tables, so a type can never render with one icon in the
 * palette and another on the results page.
 */

/** Type icons, keyed by the API's resource_type value. */
export const TYPE_ICONS: Record<FavoriteResourceType, LucideIcon> = {
  folder: Folder,
  notebook: BookOpen,
  dashboard: LayoutDashboard,
  connector: Database,
  agent: Bot,
  model_config: Cpu,
  skill: Sparkles,
  mcp_server: Plug,
  tool: Wrench,
}

/** Group-header labels (plural) for result sections, in the API's group order. */
export const TYPE_LABELS: Record<FavoriteResourceType, string> = {
  folder: 'Folders',
  notebook: 'Notebooks',
  dashboard: 'Dashboards',
  connector: 'Connectors',
  agent: 'Agents',
  model_config: 'Models',
  skill: 'Skills',
  mcp_server: 'MCP servers',
  tool: 'Tools',
}

/** Singular labels for the recents rows' meta line. */
export const TYPE_SINGULAR: Record<FavoriteResourceType, string> = {
  folder: 'Folder',
  notebook: 'Notebook',
  dashboard: 'Dashboard',
  connector: 'Connector',
  agent: 'Agent',
  model_config: 'Model',
  skill: 'Skill',
  mcp_server: 'MCP server',
  tool: 'Tool',
}

/** The resource types `/recent` can return, mapped onto the catalog union. */
export const RECENT_TYPES: Record<string, FavoriteResourceType> = {
  notebook: 'notebook',
  dashboard: 'dashboard',
  connector: 'connector',
}

/** True for the nine catalog types the client can render. */
export function isKnownType(type: string): type is FavoriteResourceType {
  return Object.prototype.hasOwnProperty.call(TYPE_ICONS, type)
}

/**
 * Where opening (or clicking) each resource type takes the user. A notebook
 * content hit passes the matched `cellId`, deep-linking to the cell so the
 * notebook page can scroll to and flash it; every other case (and every other
 * type) keeps its plain route.
 */
export function resourceHref(type: FavoriteResourceType, id: string, cellId?: string): string {
  switch (type) {
    case 'notebook': return cellId ? `/notebooks/${id}#cell-${encodeURIComponent(cellId)}` : `/notebooks/${id}`
    case 'dashboard': return `/dashboards/${id}/view`
    case 'folder': return `/?folder=${id}`
    case 'connector': return '/connectors'
    case 'agent': return '/agents'
    case 'model_config': return '/models'
    case 'skill': return '/skills'
    case 'tool': return '/tools'
    case 'mcp_server': return '/mcps'
  }
}
