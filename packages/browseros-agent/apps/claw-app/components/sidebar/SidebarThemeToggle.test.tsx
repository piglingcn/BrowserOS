import { describe, expect, it } from 'bun:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { ThemeProvider } from '@/components/theme-provider'
import { TooltipProvider } from '@/components/ui/tooltip'
import { SidebarThemeToggle } from './SidebarThemeToggle'

// No localStorage in bun tests, so the provider always starts in
// system mode; interactive behavior (opening the menu, switching
// themes) is covered by the headless agent-browser pass.
function render(expanded: boolean): string {
  return renderToStaticMarkup(
    <TooltipProvider>
      <ThemeProvider>
        <SidebarThemeToggle expanded={expanded} />
      </ThemeProvider>
    </TooltipProvider>,
  )
}

describe('SidebarThemeToggle', () => {
  it('labels the trigger with the current theme', () => {
    expect(render(false)).toContain('aria-label="Theme: System"')
  })

  it('renders the current theme icon', () => {
    expect(render(false)).toMatch(/<svg/)
  })

  it('shows the mode name when expanded', () => {
    expect(render(true)).toMatch(/opacity-100[^>]*>System</)
  })

  it('hides the mode name when collapsed', () => {
    expect(render(false)).toMatch(/opacity-0[^>]*>System</)
  })
})
