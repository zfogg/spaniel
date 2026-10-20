import * as Menu from '@radix-ui/react-dropdown-menu'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { NavLink, useLocation } from 'react-router-dom'

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

function MenuLink({ label, to }: { label: string; to: string }) {
  return (
    <Menu.Item asChild>
      <NavLink to={to} end={to === '/'} className={item}>
        {label}
      </NavLink>
    </Menu.Item>
  )
}

export default function NavigationGroups() {
  const { pathname } = useLocation()
  return (
    <>
      {groups.map((group) => {
        const active =
          group.items.some(([, to]) =>
            to === '/'
              ? pathname === '/' || pathname.startsWith('/traces/')
              : pathname.startsWith(to),
          ) ||
          (group.label === 'Tools' &&
            (pathname.startsWith('/docs/') || pathname === '/openapi-docs'))
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
                    <Menu.SubTrigger className={item}>
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
