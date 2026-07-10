import {
  ArrowRight,
  Globe,
  MessageCircle,
  ShoppingBag,
  Target,
  TrendingUp,
} from 'lucide-react'
import { type FC, useState } from 'react'
import { openSidePanelWithSearch } from '@/lib/messaging/sidepanel/openSidepanelWithSearch'

const WORKFLOW_STEPS = [
  {
    icon: <Globe className="h-4 w-4" />,
    label: '打开拼多多网页版',
    desc: '打开 mobile.yangkeduo.com 并检查登录',
  },
  {
    icon: <ShoppingBag className="h-4 w-4" />,
    label: '搜索目标商品',
    desc: '搜索指定商品关键词，按销量排序',
  },
  {
    icon: <TrendingUp className="h-4 w-4" />,
    label: '采集商品数据',
    desc: '提取销量前 N 的商品标题、售价、已拼件数',
  },
  {
    icon: <MessageCircle className="h-4 w-4" />,
    label: '分析评论',
    desc: '读取商品前 10 条买家评价，总结卖点与槽点',
  },
]

export const DemoPddPage: FC = () => {
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)

  const handleSubmit = async () => {
    const text = input.trim()
    if (!text) return

    setIsLoading(true)

    const fullPrompt = `请帮我执行一个拼多多竞品分析工作流。

用户需求：${text}

重要提醒：
- 每次操作前先缓慢移动鼠标到目标位置，不要直接瞬移点击
- 每个操作之间至少等待 3-5 秒
- 模拟人类行为：偶尔停留、滚动速度不要太快
- 如果页面出现验证码或反爬虫验证，立即停下，提示用户"请手动完成验证后再继续"

工作流步骤：

1. 打开 https://mobile.yangkeduo.com/
   - 缓慢移动鼠标到页面区域
   - 如果页面跳转到登录页，说明未登录，提示用户"请在拼多多网页版完成登录后再操作"，然后终止流程

2. 搜索目标商品
   - 在搜索框中输入用户指定的商品关键词，缓慢输入，不要粘贴
   - 点击搜索按钮，等待搜索结果加载完成
   - 按销量排序：找到排序筛选器，选择"销量优先"或"销量从高到低"

3. 采集商品数据
   - 从搜索结果中逐个提取销量排名靠前的商品数据
   - 每条商品提取：商品标题、当前售价、已拼件数、店铺名称
   - 根据用户需求 ${text} 决定采集数量（默认前 20 个）
   - 缓慢滚动加载更多商品，每次滚动后等待 2-3 秒

4. 采集买家评价
   - 逐个点击商品进入详情页
   - 找到评价/评论区，提取前 10 条买家评价内容
   - 完成后返回搜索结果列表，继续下一个商品
   - 注意：每个商品之间间隔 5-8 秒，模拟真实浏览

5. 分析并输出结果
   - 将所有数据整理为表格，包含：排名、商品标题、售价、已拼件数、店铺名、评价摘要
   - 分析价格分布，找出"黄金价格带"（销量最好的价位区间）
   - 总结竞品的主打卖点和高频关键词
   - 输出分析结论和建议

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
          <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-red-500/20 to-orange-500/20">
            <ShoppingBag className="h-7 w-7 text-red-500" />
          </div>
          <div className="space-y-1">
            <h1 className="font-semibold text-[clamp(1.5rem,3vw,2rem)] leading-[1.08]">
              拼多多竞品分析
            </h1>
            <p className="text-muted-foreground text-xs leading-5">
              搜索商品，分析竞品定价、销量和买家评价，找出黄金价格带
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
            placeholder="输入商品关键词，如：便携式桌面小风扇 采集前20个商品"
            className="min-h-[80px] w-full resize-none rounded-xl border border-border/50 bg-card px-4 py-3 text-sm outline-none transition-colors placeholder:text-muted-foreground/60 hover:border-border focus:border-red-500"
            rows={2}
          />
          <button
            type="button"
            onClick={handleSubmit}
            disabled={!input.trim() || isLoading}
            className="flex items-center justify-center gap-2 rounded-xl bg-red-500 px-6 py-2.5 text-sm font-medium text-white transition-all duration-200 hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {isLoading ? (
              <span className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent" />
            ) : (
              <>
                开始分析
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
