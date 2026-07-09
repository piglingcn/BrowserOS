import { PlugZap } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { DisplayHeading, Em, StepCopy } from '../components/DisplayHeading'
import { StarterPromptTile } from '../components/StarterPromptTile'
import { StepWrap } from '../components/StepWrap'
import { STARTER_PROMPTS } from '../onboarding-v2.helpers'
import type { ImportPhase } from '../onboarding-v2.types'

interface ReadyStepProps {
  phase: ImportPhase
  onDone: () => void
}

/** Renders the final MCP setup step and post-connection starter prompts. */
export function ReadyStep({ phase, onDone }: ReadyStepProps) {
  const didImportLogins = phase === 'imported'

  return (
    <StepWrap>
      {didImportLogins ? (
        <DisplayHeading>
          Logins <Em>imported</Em>.
        </DisplayHeading>
      ) : (
        <DisplayHeading>
          Almost <Em>there</Em>.
        </DisplayHeading>
      )}
      <StepCopy>
        {didImportLogins
          ? 'One step left: connect your agent. Open MCP in BrowserClaw and link Claude Code, Cursor, Codex, or another harness. Then your agent runs tasks here, logged in as you. You watch, approve, and audit.'
          : 'Connect your agent next. Open MCP in BrowserClaw and link Claude Code, Cursor, Codex, or another harness. Then your agent runs tasks in this browser. You watch, approve, and audit.'}
      </StepCopy>
      <div className="mb-2.5 font-bold text-[12.5px] text-ink-2">
        Once connected, try one of these.
      </div>
      <div className="mb-6 flex flex-col gap-2.5">
        {STARTER_PROMPTS.slice(0, 2).map((prompt) => (
          <StarterPromptTile key={prompt} prompt={prompt} />
        ))}
      </div>
      <Button type="button" size="lg" onClick={onDone}>
        <PlugZap className="size-4" />
        Connect your agent
      </Button>
    </StepWrap>
  )
}
