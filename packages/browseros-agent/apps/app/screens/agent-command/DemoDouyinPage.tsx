import {
  ArrowRight,
  BarChart3,
  Globe,
  MessageSquare,
  Music,
  Search,
} from 'lucide-react'
import { type FC, useState } from 'react'
import { openSidePanelWithSearch } from '@/lib/messaging/sidepanel/openSidepanelWithSearch'
import { buildDouyinWorkflowPrompt } from './douyin-workflow'

const WORKFLOW_STEPS = [
  {
    icon: <Globe className="h-4 w-4" />,
    label: '打开抖音网页版',
    desc: '打开 douyin.com 并检查登录状态',
  },
  {
    icon: <Search className="h-4 w-4" />,
    label: '搜索爆款视频',
    desc: '搜索关键词，按最多点赞筛选，找近期爆款',
  },
  {
    icon: <BarChart3 className="h-4 w-4" />,
    label: '采集互动数据',
    desc: '进入视频结果，点击筛选并选择最多点赞',
  },
  {
    icon: <MessageSquare className="h-4 w-4" />,
    label: '导出报告',
    desc: '汇总互动数据并导出 Markdown 文件',
  },
]

export const DemoDouyinPage: FC = () => {
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)

  const handleSubmit = async () => {
    const text = input.trim()
    if (!text) return

    setIsLoading(true)

    const fullPrompt = buildDouyinWorkflowPrompt(text)

    try {
      await openSidePanelWithSearch('open', {
        query: fullPrompt,
        mode: 'agent',
      })
    } catch {}
    setIsLoading(false)
  }

  return (
    <div className="min-h-full px-4 py-6">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
        {/* 标题 */}
        <div className="flex flex-col items-center gap-3 pt-[max(4vh,16px)] text-center">
          <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-pink-500/20 to-red-500/20">
            <Music className="h-7 w-7 text-pink-500" />
          </div>
          <div className="space-y-1">
            <h1 className="font-semibold text-[clamp(1.5rem,3vw,2rem)] leading-[1.08]">
              抖音热门视频
            </h1>
            <p className="text-muted-foreground text-xs leading-5">
              按最多点赞采集前 2 条视频互动数据并导出 Markdown
            </p>
          </div>
        </div>

        {/* 工作流说明 */}
        <div className="rounded-xl border border-border/50 bg-card p-4">
          <h3 className="mb-3 font-medium text-sm">自动工作流</h3>
          <div className="grid gap-3">
            {WORKFLOW_STEPS.map((step, i) => (
              <div key={step.label} className="flex items-start gap-3">
                <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-muted font-medium text-muted-foreground text-xs">
                  {i + 1}
                </div>
                <div className="flex items-start gap-2 pt-0.5">
                  <span className="mt-0.5 text-muted-foreground">
                    {step.icon}
                  </span>
                  <div>
                    <div className="font-medium text-sm">{step.label}</div>
                    <div className="text-muted-foreground text-xs">
                      {step.desc}
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* 输入框 */}
        <div className="flex flex-col gap-3">
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="输入搜索关键词，比如：美食探店"
            className="min-h-[80px] w-full resize-none rounded-xl border border-border/50 bg-card px-4 py-3 text-sm outline-none transition-colors placeholder:text-muted-foreground/60 hover:border-border focus:border-pink-500"
            rows={2}
          />
          <button
            type="button"
            onClick={handleSubmit}
            disabled={!input.trim() || isLoading}
            className="flex items-center justify-center gap-2 rounded-xl bg-pink-500 px-6 py-2.5 font-medium text-sm text-white transition-all duration-200 hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {isLoading ? (
              <span className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent" />
            ) : (
              <>
                开始采集
                <ArrowRight className="h-4 w-4" />
              </>
            )}
          </button>
        </div>

        {/* 提示 */}
        <p className="text-center text-muted-foreground/50 text-xs">
          需要已配置 AI Provider（如 Claude、GPT）才能执行浏览器自动化操作
        </p>
      </div>
    </div>
  )
}
