import {
  Bot,
  Briefcase,
  FileText,
  Image,
  Music,
  ShoppingBag,
  Sparkles,
  Video,
} from 'lucide-react'
import { type FC, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router'
import type { Provider } from '@/components/chat/chatComponentTypes'
import { Feature } from '@/lib/browseros/capabilities'
import { createBrowserOSAction } from '@/lib/chat-actions/types'
import { openSidePanelWithSearch } from '@/lib/messaging/sidepanel/openSidepanelWithSearch'
import {
  useAgentAdapters,
  useHarnessAgents,
} from '@/modules/agents/agents.hooks'
import { useCapabilities } from '@/modules/browseros/capabilities.hooks'
import { toProviderOption } from '@/modules/chat/chat-session-request'
import {
  buildSidepanelChatTargets,
  persistSidepanelChatTargetSelection,
  resolveSidepanelChatTarget,
} from '@/modules/chat/sidepanel-chat-targets'
import { useLlmProviders } from '@/modules/llm-providers/llm-providers.hooks'
import { useActiveHint } from '@/screens/newtab/index/active-hint.hooks'
import { ImportDataHint } from '@/screens/newtab/index/ImportDataHint'
import { RecentSites } from '@/screens/newtab/index/RecentSites'
import { ScheduleResults } from '@/screens/newtab/index/ScheduleResults'
import { SignInHint } from '@/screens/newtab/index/SignInHint'
import {
  ConversationInput,
  type ConversationInputSendInput,
} from './ConversationInput'
import {
  resolveHomeLlmRoutingMode,
  routeHomeSend,
} from './home-compose.helpers'
import { setPendingInitialMessage } from './pending-initial-message'

export const AgentCommandHome: FC = () => {
  const navigate = useNavigate()
  const activeHint = useActiveHint()
  const {
    providers: llmProviders,
    defaultProviderId,
    setDefaultProvider,
  } = useLlmProviders()
  const { harnessAgents } = useHarnessAgents()
  const { adapters } = useAgentAdapters()
  const { supports, isLoading: capabilitiesLoading } = useCapabilities()
  const supportsInlineChat = supports(Feature.NEWTAB_CHAT_SUPPORT)
  const llmRoutingMode = resolveHomeLlmRoutingMode({
    capabilitiesLoading,
    supportsInlineChat,
  })
  const [selectedProvider, setSelectedProvider] = useState<Provider | null>(
    null,
  )
  const waitingForLlmCapabilities =
    selectedProvider?.kind === 'llm' && llmRoutingMode === 'wait'

  const targets = useMemo(
    () =>
      buildSidepanelChatTargets({
        providers: llmProviders,
        adapters,
        agents: harnessAgents,
      }),
    [llmProviders, adapters, harnessAgents],
  )
  const providerOptions = useMemo(
    () => targets.map(toProviderOption),
    [targets],
  )

  // Default the picker to the user's default LLM provider (BrowserOS out of the
  // box) so the composer works with zero agents. Re-resolve if the current
  // selection disappears (e.g. its provider/agent was removed).
  useEffect(() => {
    if (targets.length === 0) return
    const stillValid =
      selectedProvider &&
      providerOptions.some(
        (option) =>
          option.id === selectedProvider.id &&
          option.kind === selectedProvider.kind,
      )
    if (stillValid) return
    const fallback = resolveSidepanelChatTarget({ targets, defaultProviderId })
    setSelectedProvider(fallback ? toProviderOption(fallback) : null)
  }, [targets, providerOptions, selectedProvider, defaultProviderId])

  const handleSend = async (input: ConversationInputSendInput) => {
    if (!selectedProvider) return
    if (selectedProvider.kind === 'llm' && llmRoutingMode === 'wait') return
    const agentSessionId =
      selectedProvider.kind === 'acp' ? crypto.randomUUID() : undefined
    const route = routeHomeSend(selectedProvider, input.text, {
      agentSessionId,
      selectedTabs: input.selectedTabs,
    })
    if (!route) return
    if (route.kind === 'acp') {
      if (!agentSessionId) return
      // Stash text + attachments in the in-memory registry. Text also travels
      // in `?q=` so a hard refresh / shareable URL still works for text-only
      // prompts; attachments are registry-only (a multi-MB dataUrl can't ride
      // a URL param). The chat screen prefers the registry when both exist.
      setPendingInitialMessage({
        agentId: route.agentId,
        sessionId: agentSessionId,
        text: input.text,
        attachments: input.attachments,
        createdAt: Date.now(),
      })
      navigate(route.path)
      return
    }
    const target = targets.find(
      (entry) => entry.kind === 'llm' && entry.id === route.providerId,
    )
    await persistSidepanelChatTargetSelection(target)
    await setDefaultProvider(route.providerId)
    if (llmRoutingMode === 'sidepanel') {
      const action = createBrowserOSAction({
        mode: 'chat',
        message: input.text,
        tabs: input.selectedTabs,
      })
      await openSidePanelWithSearch('open', {
        query: input.text,
        mode: 'chat',
        action,
      })
      return
    }
    navigate(route.path)
  }

  const handleQuickAction = (text: string) => {
    if (selectedProvider) {
      handleSend({ text, attachments: [], selectedTabs: [] })
    } else {
      navigate(`/home/chat?q=${encodeURIComponent(text)}&mode=chat`)
    }
  }

  return (
    <div className="min-h-full px-4 py-6">
      <div className="mx-auto flex w-full max-w-5xl flex-col gap-8">
        {/* 大图标快捷入口 */}
        <div className="flex flex-col items-center gap-6 pt-[max(8vh,20px)] text-center">
          <div className="grid grid-cols-3 gap-6">
            <button
              type="button"
              onClick={() => navigate('/home/chat?mode=chat')}
              className="flex flex-col items-center gap-3 rounded-2xl border border-border/50 bg-card p-6 transition-all duration-200 hover:-translate-y-1 hover:border-[var(--accent-orange)]/50 hover:bg-[var(--accent-orange)]/5 hover:shadow-lg"
            >
              <div className="flex h-16 w-16 items-center justify-center rounded-xl bg-gradient-to-br from-blue-500/20 to-purple-500/20">
                <Sparkles className="h-8 w-8 text-[var(--accent-orange)]" />
              </div>
              <span className="font-medium text-sm">AI 对话</span>
            </button>

            <button
              type="button"
              onClick={() => handleQuickAction('生成一张图片：')}
              className="flex flex-col items-center gap-3 rounded-2xl border border-border/50 bg-card p-6 transition-all duration-200 hover:-translate-y-1 hover:border-green-500/50 hover:bg-green-500/5 hover:shadow-lg"
            >
              <div className="flex h-16 w-16 items-center justify-center rounded-xl bg-gradient-to-br from-green-500/20 to-emerald-500/20">
                <Image className="h-8 w-8 text-green-500" />
              </div>
              <span className="font-medium text-sm">图片生成</span>
            </button>

            <button
              type="button"
              onClick={() => handleQuickAction('帮我写一篇文案：')}
              className="flex flex-col items-center gap-3 rounded-2xl border border-border/50 bg-card p-6 transition-all duration-200 hover:-translate-y-1 hover:border-purple-500/50 hover:bg-purple-500/5 hover:shadow-lg"
            >
              <div className="flex h-16 w-16 items-center justify-center rounded-xl bg-gradient-to-br from-purple-500/20 to-pink-500/20">
                <FileText className="h-8 w-8 text-purple-500" />
              </div>
              <span className="font-medium text-sm">写文案</span>
            </button>

            <button
              type="button"
              onClick={() => navigate('/demo-image')}
              className="flex flex-col items-center gap-3 rounded-2xl border border-border/50 bg-card p-6 transition-all duration-200 hover:-translate-y-1 hover:border-[var(--accent-orange)]/50 hover:bg-[var(--accent-orange)]/5 hover:shadow-lg"
            >
              <div className="flex h-16 w-16 items-center justify-center rounded-xl bg-gradient-to-br from-rose-500/20 to-orange-500/20">
                <Bot className="h-8 w-8 text-[var(--accent-orange)]" />
              </div>
              <span className="font-medium text-sm">图片生成(DS+即梦)</span>
            </button>

            <button
              type="button"
              onClick={() => navigate('/demo-video')}
              className="flex flex-col items-center gap-3 rounded-2xl border border-border/50 bg-card p-6 transition-all duration-200 hover:-translate-y-1 hover:border-amber-500/50 hover:bg-amber-500/5 hover:shadow-lg"
            >
              <div className="flex h-16 w-16 items-center justify-center rounded-xl bg-gradient-to-br from-amber-500/20 to-yellow-500/20">
                <Video className="h-8 w-8 text-amber-500" />
              </div>
              <span className="font-medium text-sm">视频生成(DS+SD2)</span>
            </button>

            <button
              type="button"
              onClick={() => navigate('/demo-resume')}
              className="flex flex-col items-center gap-3 rounded-2xl border border-border/50 bg-card p-6 transition-all duration-200 hover:-translate-y-1 hover:border-blue-500/50 hover:bg-blue-500/5 hover:shadow-lg"
            >
              <div className="flex h-16 w-16 items-center justify-center rounded-xl bg-gradient-to-br from-blue-500/20 to-indigo-500/20">
                <Briefcase className="h-8 w-8 text-blue-500" />
              </div>
              <span className="font-medium text-sm">BOSS直聘</span>
            </button>

            <button
              type="button"
              onClick={() => navigate('/demo-douyin')}
              className="flex flex-col items-center gap-3 rounded-2xl border border-border/50 bg-card p-6 transition-all duration-200 hover:-translate-y-1 hover:border-pink-500/50 hover:bg-pink-500/5 hover:shadow-lg"
            >
              <div className="flex h-16 w-16 items-center justify-center rounded-xl bg-gradient-to-br from-pink-500/20 to-red-500/20">
                <Music className="h-8 w-8 text-pink-500" />
              </div>
              <span className="font-medium text-sm">抖音热门</span>
            </button>

            <button
              type="button"
              onClick={() => navigate('/demo-pdd')}
              className="flex flex-col items-center gap-3 rounded-2xl border border-border/50 bg-card p-6 transition-all duration-200 hover:-translate-y-1 hover:border-red-500/50 hover:bg-red-500/5 hover:shadow-lg"
            >
              <div className="flex h-16 w-16 items-center justify-center rounded-xl bg-gradient-to-br from-red-500/20 to-orange-500/20">
                <ShoppingBag className="h-8 w-8 text-red-500" />
              </div>
              <span className="font-medium text-sm">拼多多分析</span>
            </button>
          </div>

          {/* 输入框 */}
          <div className="w-full max-w-3xl">
            <ConversationInput
              variant="home"
              providers={providerOptions}
              selectedProvider={selectedProvider}
              onSelectProvider={setSelectedProvider}
              onSend={handleSend}
              streaming={false}
              disabled={!selectedProvider || waitingForLlmCapabilities}
              attachmentsEnabled={true}
              placeholder={
                selectedProvider
                  ? `向 ${selectedProvider.name} 提出需求...`
                  : '加载中...'
              }
              onOpenVoiceMode={() => {
                navigate('/home/chat?voice=open&mode=chat')
              }}
            />
          </div>
        </div>

        {/* 原标题区域 - 翻译成中文往下移 */}
        <div className="flex flex-col items-center gap-5 pt-8 text-center">
          <div className="space-y-3">
            <h1 className="font-semibold text-[clamp(1.5rem,3vw,2.25rem)] text-muted-foreground/60 leading-[1.08] tracking-[-0.025em] [text-wrap:balance]">
              想让你的 Agent{' '}
              <span className="font-medium text-[var(--accent-orange)] italic">
                做些什么
              </span>{' '}
              呢？
            </h1>
            <p className="mx-auto max-w-2xl text-muted-foreground/40 text-xs leading-5 [text-wrap:pretty]">
              选择 BrowserOS AI 或任意 Agent，然后开始一个任务 —
              无需离开当前标签页
            </p>
          </div>
        </div>

        <div className="mx-auto flex w-full max-w-3xl flex-col gap-10 pb-12">
          <RecentSites />
          <ScheduleResults />
        </div>
      </div>

      {activeHint === 'signin' ? <SignInHint /> : null}
      {activeHint === 'import' ? <ImportDataHint /> : null}
    </div>
  )
}
