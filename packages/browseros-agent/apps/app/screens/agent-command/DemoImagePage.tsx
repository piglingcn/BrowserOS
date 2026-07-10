import { type FC, useState } from 'react'
import { Bot, Sparkles, ArrowRight, Globe, Zap } from 'lucide-react'
import { openSidePanelWithSearch } from '@/lib/messaging/sidepanel/openSidepanelWithSearch'

const WORKFLOW_STEPS = [
  { icon: <Globe className="h-4 w-4" />, label: '登录 DeepSeek', desc: '打开 deepseek.com 并登录' },
  { icon: <Zap className="h-4 w-4" />, label: '优化提示词', desc: '在 DeepSeek 中让 AI 优化你的图片描述，生成适合即梦的生图提示词' },
  { icon: <Globe className="h-4 w-4" />, label: '打开即梦', desc: '打开即梦网页，进入 AI 图片生成' },
  { icon: <Sparkles className="h-4 w-4" />, label: '生成图片', desc: '将优化后的提示词提交给即梦，生成并展示结果' },
]

export const DemoImagePage: FC = () => {
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)

  const handleSubmit = async () => {
    const text = input.trim()
    if (!text) return

    setIsLoading(true)

    // 构建带有完整工作流指令的消息
    const fullPrompt = `请帮我执行一个 AI 图片生成工作流。

用户需求：${text}

工作流步骤：
1. 打开 https://chat.deepseek.com/
2. 检查是否已登录：查看当前页面 URL 是否包含 "sign_in"。如果 URL 中有 "sign_in" 说明未登录，提示用户"请在 DeepSeek 页面完成登录"，然后每10秒检查一次 URL，最多4次（40秒），如果4次后仍未登录则停止
3. 登录成功后，在 DeepSeek 的输入框中粘贴以下内容（**使用粘贴操作，不要逐字输入**）：
   "请直接输出适合即梦（jimeng.jianying.com）AI 图片生成的中文提示词，不要输出其他内容。要求：详细描述画面构图、光影、色彩、风格。注意：1. 自动规避所有敏感词（血腥、暴力、阴暗、攻击性等）2. 优先选择'插画/3D卡通/水彩/盲盒/皮克斯风格'等艺术化风格，降低写实风格触发审核的概率。我的需求是：${text}"
4. 找到最右侧发送按钮并点击
5. **等待 DeepSeek 完全回复完成**，不要提前进行下一步。确认回复内容完整后，提取最适合的生图提示词
5. 确认生图提示词已到手后，在当前标签页打开 https://jimeng.jianying.com/（不要打开新窗口或新标签页）
6. 即梦页面加载后，即梦默认是 Agent 模式或者是视频生成，需要先点击切换到"图片生成"模式
7. 在图片生成模式的输入框中**粘贴** DeepSeek 优化后的提示词（使用粘贴操作，不要逐字输入）
8. 点击生成按钮。
9. 如果没有弹出登录窗口，则等待图片生成完成。如果点击时弹出了登录窗口，说明未登录，提示用户"请在即梦页面完成登录（手机号验证）"，然后每10秒检查一次登录弹窗是否关闭，最多4次（40秒），如果4次后登录弹窗仍在则停止任务。
10. 图片生成后，截图获取生成的图片结果展示给用户
11. 如果生成失败，尝试再次点击生成按钮，最多重试2次。**注意：不要切换模型或修改任何参数，只需重新点击生成按钮**

每一步向用户报告进度。不要替用户输入登录信息。关键：每一步完成后再进行下一步，不要跳步。`

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
          <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-rose-500/20 to-orange-500/20">
            <Bot className="h-7 w-7 text-[var(--accent-orange)]" />
          </div>
          <div className="space-y-1">
            <h1 className="font-semibold text-[clamp(1.5rem,3vw,2rem)] leading-[1.08]">
              AI 图片生成(DS+即梦)
            </h1>
            <p className="text-muted-foreground text-xs leading-5">
              描述你想生成的图片，DeepSeek 优化提示词 → 即梦出图
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
            placeholder="描述你想生成的图片，比如：一只橘猫坐在窗台上，夕阳洒进来，温暖的感觉..."
            className="min-h-[100px] w-full resize-none rounded-xl border border-border/50 bg-card px-4 py-3 text-sm outline-none transition-colors placeholder:text-muted-foreground/60 hover:border-border focus:border-[var(--accent-orange)]"
            rows={4}
          />
          <button
            type="button"
            onClick={handleSubmit}
            disabled={!input.trim() || isLoading}
            className="flex items-center justify-center gap-2 rounded-xl bg-[var(--accent-orange)] px-6 py-2.5 text-sm font-medium text-white transition-all duration-200 hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {isLoading ? (
              <span className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent" />
            ) : (
              <>
                开始生成
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
