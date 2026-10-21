import * as Menu from '@radix-ui/react-dropdown-menu'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { Link, useLocation } from 'react-router-dom'

const groups = [
  {
    label: 'Telemetry',
    items: [
      ['Traces', '/'],
      ['Spans', '/spans'],
      ['Logs', '/logs'],
      ['Metrics', '/metrics'],
    ],
  },
  {
    label: 'Monitor',
    items: [
      ['Dashboards', '/dashboards'],
      ['Alerts', '/alerts'],
    ],
  },
  {
    label: 'Tools',
    items: [
      ['Coverage', '/coverage'],
      ['Lint', '/lint'],
    ],
  },
]
const docs = [
  ['OpenAPI docs', '/openapi-docs'],
  ['OpenAPI spec file', '/docs/openapi-spec'],
  ['Database schema', '/docs/database'],
]
const content = 'z-[70] min-w-44 rounded-md border border-border bg-background p-1 shadow-lg'
const item =
  'flex items-center justify-between gap-3 rounded px-3 py-2 text-xs text-muted-foreground outline-none data-[highlighted]:bg-muted data-[highlighted]:text-foreground [&.active]:bg-muted [&.active]:text-foreground'

function isSectionActive(pathname: string, to: string): boolean {
  if (to === '/') return pathname === '/' || isSectionActive(pathname, '/traces')
  return pathname === to || pathname.startsWith(`${to}/`)
}

function MenuLink({ label, to }: { label: string; to: string }) {
  const { pathname } = useLocation()
  const active = isSectionActive(pathname, to)
  return (
    <Menu.Item asChild>
      <Link
        to={to}
        aria-current={active ? 'page' : undefined}
        className={`${item}${active ? ' active' : ''}`}
      >
        {label}
      </Link>
    </Menu.Item>
  )
}

export default function NavigationGroups() {
  const { pathname } = useLocation()
  return (
    <>
      {groups.map((group) => {
        const docsActive = docs.some(([, to]) => isSectionActive(pathname, to))
        const active =
          group.items.some(([, to]) => isSectionActive(pathname, to)) ||
          (group.label === 'Tools' && docsActive)
        return (
          <Menu.Root key={group.label}>
            <Menu.Trigger
              aria-current={active ? 'page' : undefined}
              className={`inline-flex items-center gap-1 rounded-md border px-2.5 py-[5px] text-xs font-medium ${active ? 'border-border bg-muted text-foreground' : 'border-transparent text-muted-foreground hover:bg-muted'}`}
            >
              {group.label}
              <ChevronDown size={13} />
            </Menu.Trigger>
            <Menu.Portal>
              <Menu.Content className={content} align="start" sideOffset={5}>
                {group.items.map(([label, to]) => (
                  <MenuLink key={to} label={label} to={to} />
                ))}
                {group.label === 'Tools' && (
                  <Menu.Sub>
                    <Menu.SubTrigger
                      aria-current={docsActive ? 'page' : undefined}
                      className={`${item}${docsActive ? ' active' : ''}`}
                    >
                      Docs
                      <ChevronRight size={13} />
                    </Menu.SubTrigger>
                    <Menu.Portal>
                      <Menu.SubContent className={content} sideOffset={4}>
                        {docs.map(([label, to]) => (
                          <MenuLink key={to} label={label} to={to} />
                        ))}
                      </Menu.SubContent>
                    </Menu.Portal>
                  </Menu.Sub>
                )}
              </Menu.Content>
            </Menu.Portal>
          </Menu.Root>
        )
      })}
    </>
  )
}
