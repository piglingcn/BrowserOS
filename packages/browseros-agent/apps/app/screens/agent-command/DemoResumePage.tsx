import { type FC, useState } from 'react'
import { Download, Globe, MessageCircle, FileText, ArrowRight, Briefcase } from 'lucide-react'
import { openSidePanelWithSearch } from '@/lib/messaging/sidepanel/openSidepanelWithSearch'

const WORKFLOW_STEPS = [
  { icon: <Globe className="h-4 w-4" />, label: '打开 BOSS直聘', desc: '打开 zhipin.com 并检查登录状态' },
  { icon: <MessageCircle className="h-4 w-4" />, label: '进入沟通列表', desc: '找到已沟通的候选人会话列表' },
  { icon: <FileText className="h-4 w-4" />, label: '遍历简历', desc: '逐个查看候选人附件简历' },
  { icon: <Download className="h-4 w-4" />, label: '批量下载', desc: '下载简历并命名为 姓名-职位.pdf' },
]

export const DemoResumePage: FC = () => {
  const [input, setInput] = useState('50')
  const [isLoading, setIsLoading] = useState(false)

  const handleSubmit = async () => {
    const text = input.trim()
    if (!text) return

    setIsLoading(true)

    const fullPrompt = `请帮我执行一个 BOSS直聘操作流程。

1. 打开 https://www.zhipin.com/web/chat/index
   - 如果页面跳转到登录页，说明未登录，提示用户"请先在 BOSS直聘 网页完成登录后再重新操作"，然后终止流程

2. 页面加载后，右侧显示已沟通用户列表（会话列表）
   - 忽略列表顶部可能出现的广告内容
   - 找到第一个正常的用户会话项（显示姓名、职位、最近对话时间），点击进入该会话

3. 进入会话后，停止操作，向用户报告已成功进入第一个用户的会话页面。

请不要做任何多余的操作，只完成以上步骤即可。`

    try {
      await openSidePanelWithSearch('open', {
        query: fullPrompt,
        mode: 'agent',
      })
    } catch (err) {
      console.error('openSidePanelWithSearch failed:', err)
    }
    setIsLoading(false)
  }

  return (
    <div className="min-h-full px-4 py-6">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
        {/* 标题 */}
        <div className="flex flex-col items-center gap-3 pt-[max(4vh,16px)] text-center">
          <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-blue-500/20 to-indigo-500/20">
            <Briefcase className="h-7 w-7 text-blue-500" />
          </div>
          <div className="space-y-1">
            <h1 className="font-semibold text-[clamp(1.5rem,3vw,2rem)] leading-[1.08]">
              BOSS简历批量下载
            </h1>
            <p className="text-muted-foreground text-xs leading-5">
              批量下载 BOSS直聘中已沟通候选人的附件简历
            </p>
          </div>
        </div>

        {/* 工作流说明 */}
        <div className="rounded-xl border border-border/50 bg-card p-4">
          <h3 className="mb-3 font-medium text-sm">自动工作流</h3>
          <div className="grid gap-3">
            {WORKFLOW_STEPS.map((step, i) => (
              <div key={step.label} className="flex items-start gap-3">
                <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground text-xs font-medium">
                  {i + 1}
                </div>
                <div className="flex items-start gap-2 pt-0.5">
                  <span className="mt-0.5 text-muted-foreground">{step.icon}</span>
                  <div>
                    <div className="font-medium text-sm">{step.label}</div>
                    <div className="text-muted-foreground text-xs">{step.desc}</div>
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
            placeholder="期望下载的简历数量，默认50份"
            className="min-h-[80px] w-full resize-none rounded-xl border border-border/50 bg-card px-4 py-3 text-sm outline-none transition-colors placeholder:text-muted-foreground/60 hover:border-border focus:border-blue-500"
            rows={2}
          />
          <button
            type="button"
            onClick={handleSubmit}
            disabled={!input.trim() || isLoading}
            className="flex items-center justify-center gap-2 rounded-xl bg-blue-500 px-6 py-2.5 text-sm font-medium text-white transition-all duration-200 hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {isLoading ? (
              <span className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent" />
            ) : (
              <>
                开始下载
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
