import { Monitor, Moon, Sun } from 'lucide-react'
import type { ComponentType, SVGProps } from 'react'
import { useTheme } from '@/components/theme-provider'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { normalizeTheme, type Theme } from '@/lib/theme/theme-storage'
import { cn } from '@/lib/utils'

interface ThemeOption {
  value: Theme
  label: string
  icon: ComponentType<SVGProps<SVGSVGElement>>
}

const themeOptions: ThemeOption[] = [
  { value: 'system', label: 'System', icon: Monitor },
  { value: 'light', label: 'Light', icon: Sun },
  { value: 'dark', label: 'Dark', icon: Moon },
]

export interface SidebarThemeToggleProps {
  expanded?: boolean
}

/**
 * Sidebar-footer theme control. The trigger mirrors the nav-item idiom
 * (icon rail + fading label) and shows the selected *preference* —
 * Monitor in system mode even when that resolves to dark — matching
 * the apps/app toggle; the page itself shows the result.
 */
export function SidebarThemeToggle({
  expanded = false,
}: SidebarThemeToggleProps) {
  const { theme, setTheme } = useTheme()
  const current =
    themeOptions.find((option) => option.value === theme) ?? themeOptions[0]
  const CurrentIcon = current.icon

  const trigger = (
    <DropdownMenuTrigger
      aria-label={`Theme: ${current.label}`}
      className="flex h-9 w-full items-center gap-3 overflow-hidden whitespace-nowrap rounded-md px-2.5 font-medium text-sm transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground aria-expanded:bg-sidebar-accent aria-expanded:text-sidebar-accent-foreground"
    >
      <CurrentIcon className="size-5 shrink-0" />
      <span
        className={cn(
          'truncate transition-opacity duration-200',
          expanded ? 'opacity-100' : 'opacity-0',
        )}
      >
        {current.label}
      </span>
    </DropdownMenuTrigger>
  )

  return (
    <DropdownMenu modal={false}>
      {expanded ? (
        trigger
      ) : (
        <Tooltip>
          <TooltipTrigger render={trigger} />
          <TooltipContent side="right">Theme</TooltipContent>
        </Tooltip>
      )}
      <DropdownMenuContent
        side={expanded ? 'top' : 'right'}
        align={expanded ? 'start' : 'end'}
        sideOffset={8}
      >
        <DropdownMenuRadioGroup
          value={theme}
          onValueChange={(value) => setTheme(normalizeTheme(value))}
        >
          {themeOptions.map(({ value, label, icon: Icon }) => (
            <DropdownMenuRadioItem key={value} value={value}>
              <Icon />
              {label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
