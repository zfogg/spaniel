// @vitest-environment jsdom
import { useState } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { NewDashboardStarter } from './NewDashboardStarter'
import { dashboardTemplates } from './dashboard-templates'

afterEach(cleanup)
it('defaults to None and allows switching without overwriting personal text', () => {
  function Harness() {
    const [templateId, setTemplateId] = useState('none')
    const [name, setName] = useState('Office on call')
    const [description, setDescription] = useState('My notes')
    return (
      <NewDashboardStarter
        {...{ templateId, setTemplateId, name, setName, description, setDescription }}
        create={vi.fn()}
        pending={false}
        error=""
        importYAML={vi.fn()}
        importing={false}
        importError=""
      />
    )
  }
  render(<Harness />)
  expect((screen.getByRole('radio', { name: 'None' }) as HTMLInputElement).checked).toBe(true)
  for (const template of dashboardTemplates.slice(1)) {
    fireEvent.click(screen.getByRole('radio', { name: template.name }))
    expect((screen.getByRole('radio', { name: template.name }) as HTMLInputElement).checked).toBe(
      true,
    )
    expect(screen.getAllByRole('listitem')).toHaveLength(template.panels.length)
    expect((screen.getByRole('textbox', { name: /^Name$/ }) as HTMLInputElement).value).toBe(
      'Office on call',
    )
  }
  fireEvent.click(screen.getByRole('radio', { name: 'None' }))
  expect(screen.queryByRole('list')).toBeNull()
  expect((screen.getByRole('textbox', { name: /Description/ }) as HTMLTextAreaElement).value).toBe(
    'My notes',
  )
})
it('shows a create failure and disables changes during submission', () => {
  render(
    <NewDashboardStarter
      name=""
      setName={vi.fn()}
      description=""
      setDescription={vi.fn()}
      templateId="none"
      setTemplateId={vi.fn()}
      create={vi.fn()}
      pending
      error="Network unavailable"
      importYAML={vi.fn()}
      importing={false}
      importError=""
    />,
  )
  expect(
    (screen.getByRole('button', { name: 'Creating dashboard…' }) as HTMLButtonElement).disabled,
  ).toBe(true)
  expect(screen.getByRole('alert').textContent).toBe('Network unavailable')
})
