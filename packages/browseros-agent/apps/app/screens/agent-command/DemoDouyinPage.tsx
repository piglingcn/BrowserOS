import {
  ArrowRight,
  Download,
  Globe,
  Music,
  Search,
  Sparkles,
} from 'lucide-react'
import { type FC, useState } from 'react'
import { openSidePanelWithSearch } from '@/lib/messaging/sidepanel/openSidepanelWithSearch'

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
    icon: <Download className="h-4 w-4" />,
    label: '采集与下载',
    desc: '提取视频信息、源地址并下载保存',
  },
  {
    icon: <Sparkles className="h-4 w-4" />,
    label: '整理结果',
    desc: '汇总采集到的视频信息和下载路径',
  },
]

export const DemoDouyinPage: FC = () => {
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)

  const handleSubmit = async () => {
    const text = input.trim()
    if (!text) return

    setIsLoading(true)

    const fullPrompt = `请帮我执行一个抖音爆款视频搜索与下载工作流。

用户需求：${text}

重要提醒：
- 每次操作前先缓慢移动鼠标到目标位置，不要直接瞬移点击
- 每个操作之间至少等待 3-5 秒
- 每采集 3 个视频后，休息 8-10 秒
- 模拟人类行为：偶尔停留、滚动速度不要太快
- 如果页面出现验证码或反爬虫验证，立即停下，提示用户"请手动完成验证后再继续"

工作流步骤：

1. 打开 https://www.douyin.com/
   - 缓慢移动鼠标到页面区域
   - 如果页面跳转到登录页，说明未登录，提示用户"请在抖音网页版完成登录后再操作"，然后终止流程

2. 搜索爆款视频
   - 在顶部搜索框中输入用户指定的关键词，缓慢输入，不要粘贴
   - 点击搜索按钮或按回车，等待搜索结果加载
   - 在搜索结果页找到筛选/排序选项，选择"最多点赞"排序方式
   - 如果有时间筛选，选择"半年内"或"一周内"（视需求而定），优先看近期高赞内容
   - 根据用户需求 ${text} 决定采集数量

3. 采集视频数据并下载（每个视频一个独立目录，注意内存控制）
   - 逐个打开目标视频的详情页
   - 采集以下数据：视频标题、作者昵称、点赞数、收藏量、评论数

   【下载方式 — 优先 curl 直链下载】
   a) 首选方法：用 evaluate 获取 video.currentSrc（浏览器解析后的真实 CDN 地址）：
      * document.querySelector('video')?.currentSrc
      * 这个通常是 https://v3-dy-o.zjcdn.com/... 这样的真实 HTTP 地址
      * 拿到后用 run 执行 curl 下载：curl "currentSrc地址" --referer "https://www.douyin.com/" -o "文件名.mp4"
      * curl 下载走的是服务端命令行，不占用浏览器内存，最快最省

   b) 如果 currentSrc 也是 blob，尝试用 evaluate 从页面网络请求记录中提取真实视频地址：
      * performance.getEntriesByType('resource') 过滤出包含 .mp4 或 video 的请求
      * 提取请求的 name 属性即为真实 CDN 地址
      * 同样用 curl 下载

   c) 如果以上都拿不到真实地址，最后手段才用 blob fetch 分片下载（注意控制内存）

   【内存控制要点】
   - 一次只下载一个视频，严禁并行
   - 每个视频下载完成后等待 3-5 秒让内存释放
   - 优先使用方案 a) 或 b)，方案 c) 是最后手段

   【目录结构】
   d:Desktopai-browser-work\
     └─ 视频标题（点赞数N万）/
         ├─ 视频标题.mp4
         └─ 数据分析.xlsx

4. 采集评论数据
   - 在视频详情页中找到评论区
   - 提取前 100 条评论内容
   - 将评论数据整理到 数据分析.xlsx 中，包含：评论序号、评论内容、点赞数、评论时间

5. 输出最终结果
   - 每个视频一个独立目录，目录名格式：视频标题（点赞数）
   - 目录内包含：
     * 视频文件（视频标题.mp4）
     * 数据分析文件（数据分析.xlsx），包含两个sheet：
       - Sheet1「视频概览」：标题、作者、点赞数、收藏量、评论数、下载路径
       - Sheet2「评论详情」：前100条评论的序号、内容、点赞数、时间
   - 所有文件保存到 d:Desktopai-browser-work 下

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
          <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-pink-500/20 to-red-500/20">
            <Music className="h-7 w-7 text-pink-500" />
          </div>
          <div className="space-y-1">
            <h1 className="font-semibold text-[clamp(1.5rem,3vw,2rem)] leading-[1.08]">
              抖音热门视频
            </h1>
            <p className="text-muted-foreground text-xs leading-5">
              采集抖音热门视频信息，包括标题、作者、播放量等
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
            placeholder="描述你想采集的热门视频类型或数量，比如：采集前50个热门视频"
            className="min-h-[80px] w-full resize-none rounded-xl border border-border/50 bg-card px-4 py-3 text-sm outline-none transition-colors placeholder:text-muted-foreground/60 hover:border-border focus:border-pink-500"
            rows={2}
          />
          <button
            type="button"
            onClick={handleSubmit}
            disabled={!input.trim() || isLoading}
            className="flex items-center justify-center gap-2 rounded-xl bg-pink-500 px-6 py-2.5 text-sm font-medium text-white transition-all duration-200 hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
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
